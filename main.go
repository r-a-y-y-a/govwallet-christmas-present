package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/hanju/govtech-christmas/api"
	"github.com/hanju/govtech-christmas/api/cache"
	_ "github.com/lib/pq"
	goredis "github.com/redis/go-redis/v9"
)

type StaffMapping struct {
	ID          int
	StaffPassID string
	TeamName    string
	CreatedAt   int64
}

func main() {
	log.Println("Starting GovTech Christmas Redemption API Server...")

	db, err := initDB()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	redisClient, err := initRedis()
	if err != nil {
		log.Fatal("Failed to connect to Redis:", err)
	}
	defer redisClient.Close()

	redisCache := cache.NewRedisCache(redisClient)
	app := &api.App{DB: db, Cache: redisCache}

	if err := createTables(db); err != nil {
		log.Fatal("Failed to create tables:", err)
	}

	if err := loadCSVData(db); err != nil {
		log.Printf("Warning: Failed to load CSV data: %v", err)
	}

	// Pre-warm Redis cache with staff mappings from DB
	if err := prewarmCache(db, redisCache); err != nil {
		log.Printf("Warning: Failed to pre-warm cache: %v", err)
	}

	router := api.SetupRoutes(app)

	port := getEnvOrDefault("APP_PORT", "8080")
	log.Printf("Server starting on port %s", port)

	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func initRedis() (*goredis.Client, error) {
	host := getEnvOrDefault("REDIS_HOST", "localhost")
	port := getEnvOrDefault("REDIS_PORT", "6379")
	password := getEnvOrDefault("REDIS_PASSWORD", "")
	dbStr := getEnvOrDefault("REDIS_DB", "0")

	dbNum, err := strconv.Atoi(dbStr)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB value: %v", err)
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: password,
		DB:       dbNum,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ping Redis: %v", err)
	}

	log.Println("Successfully connected to Redis")
	return client, nil
}

func initDB() (*sql.DB, error) {
	host := getEnvOrDefault("DB_HOST", "localhost")
	port := getEnvOrDefault("DB_PORT", "5432")
	dbName := getEnvOrDefault("DB_NAME", "govtech_christmas")
	user := getEnvOrDefault("DB_USER", "postgres")
	password := getEnvOrDefault("DB_PASSWORD", "password123")

	psqlInfo := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		host, port, dbName, user, password)

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	log.Println("Successfully connected to database")
	return db, nil
}

func createTables(db *sql.DB) error {
	createTableSQL := `
	-- Create the staff pass mappings table
	CREATE TABLE IF NOT EXISTS staff_mappings (
		id SERIAL PRIMARY KEY,
		staff_pass_id VARCHAR(255) NOT NULL,
		team_name VARCHAR(255) NOT NULL,
		created_at BIGINT NOT NULL,
		UNIQUE(staff_pass_id, created_at)
	);

	-- Create the main redemptions table
	CREATE TABLE IF NOT EXISTS redemptions (
		team_name VARCHAR(255) UNIQUE NOT NULL,
		redeemed BOOLEAN NOT NULL DEFAULT FALSE,
		redeemed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_staff_mappings_staff_pass_id ON staff_mappings(staff_pass_id);
	CREATE INDEX IF NOT EXISTS idx_staff_mappings_team_name ON staff_mappings(team_name);
	CREATE INDEX IF NOT EXISTS idx_staff_mappings_created_at ON staff_mappings(created_at);
	CREATE INDEX IF NOT EXISTS idx_redemptions_team_name ON redemptions(team_name);
	CREATE INDEX IF NOT EXISTS idx_redemptions_redeemed_at ON redemptions(redeemed_at);
	`

	_, err := db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("error creating tables: %v", err)
	}

	log.Println("Database tables created successfully")
	return nil
}

func loadCSVData(db *sql.DB) error {
	return loadStaffMappingsFromPath(db, "./data/staff_mappings.csv")
}

func loadStaffMappingsFromPath(db *sql.DB, csvFilePath string) error {
	if _, err := os.Stat(csvFilePath); os.IsNotExist(err) {
		log.Printf("CSV file not found: %s", csvFilePath)
		return nil // Not an error, just no data to load
	}

	file, err := os.Open(csvFilePath)
	if err != nil {
		return fmt.Errorf("error opening CSV file: %v", err)
	}
	defer file.Close()

	mappings, err := parseStaffMappingsCSV(file)
	if err != nil {
		return err
	}

	for i, mapping := range mappings {
		if err := insertStaffMapping(db, mapping); err != nil {
			log.Printf("Error inserting staff mapping at line %d: %v", i+2, err)
		}
	}

	log.Printf("Loaded staff mappings from %s", csvFilePath)
	log.Println("CSV data loaded successfully")
	return nil
}

// parseStaffMappingsCSV parses CSV data from any io.Reader into a slice of StaffMapping.
// It skips the header row and logs (but does not abort on) malformed records.
func parseStaffMappingsCSV(r io.Reader) ([]StaffMapping, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // Allow variable field counts; invalid rows are filtered below
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("error reading CSV file: %v", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	var mappings []StaffMapping
	// Skip header row
	for i, record := range records[1:] {
		if len(record) != 3 {
			log.Printf("Skipping invalid record at line %d: expected 3 fields, got %d", i+2, len(record))
			continue
		}

		createdAt, err := strconv.ParseInt(record[2], 10, 64)
		if err != nil {
			log.Printf("Skipping invalid timestamp at line %d: %v", i+2, err)
			continue
		}

		mappings = append(mappings, StaffMapping{
			StaffPassID: record[0],
			TeamName:    record[1],
			CreatedAt:   createdAt,
		})
	}

	return mappings, nil
}

func insertStaffMapping(db *sql.DB, mapping StaffMapping) error {
	_, err := db.Exec(`
		INSERT INTO staff_mappings (staff_pass_id, team_name, created_at) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (staff_pass_id, created_at)
 DO NOTHING`,
		mapping.StaffPassID, mapping.TeamName, mapping.CreatedAt)
	return err
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// prewarmCache loads all staff mappings from PostgreSQL into Redis
// so the first requests don't suffer cold-start cache misses.
func prewarmCache(db *sql.DB, c cache.CacheStore) error {
	ctx := context.Background()
	rows, err := db.Query(`
		SELECT DISTINCT ON (staff_pass_id) staff_pass_id, team_name
		FROM staff_mappings
		ORDER BY staff_pass_id, created_at DESC`)
	if err != nil {
		return fmt.Errorf("failed to query staff mappings for cache prewarm: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var staffPassID, teamName string
		if err := rows.Scan(&staffPassID, &teamName); err != nil {
			log.Printf("prewarm: skipping row: %v", err)
			continue
		}
		if err := c.SetStaffTeam(ctx, staffPassID, teamName); err != nil {
			log.Printf("prewarm: failed to cache staff:%s: %v", staffPassID, err)
			continue
		}
		count++
	}

	log.Printf("Pre-warmed Redis cache with %d staff mappings", count)
	return rows.Err()
}
