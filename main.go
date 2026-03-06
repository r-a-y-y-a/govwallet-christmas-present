package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

type App struct {
	DB *sql.DB
}

type StaffMapping struct {
	ID          int    `json:"id" db:"id"`
	StaffPassID string `json:"staff_pass_id" db:"staff_pass_id"`
	TeamName    string `json:"team_name" db:"team_name"`
	CreatedAt   int64  `json:"created_at" db:"created_at"`
}

type Redemption struct {
	ID          int       `json:"id" db:"id"`
	TeamName    string    `json:"team_name" db:"team_name"`
	RedeemedAt  time.Time `json:"redeemed_at" db:"redeemed_at"`
	StaffPassID string    `json:"staff_pass_id" db:"staff_pass_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

func main() {
	log.Println("Starting GovTech Christmas Redemption System...")

	// Initialize database connection
	db, err := initDB()
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Create app instance
	app := &App{DB: db}

	// Initialize tables
	if err := app.createTables(); err != nil {
		log.Fatal("Failed to create tables:", err)
	}

	// Load CSV data if exists
	if err := app.loadCSVData(); err != nil {
		log.Printf("Warning: Failed to load CSV data: %v", err)
	}

	// Setup HTTP server
	router := setupRoutes(app)
	
	port := getEnvOrDefault("APP_PORT", "8080")
	log.Printf("Server starting on port %s", port)
	
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatal("Server failed to start:", err)
	}
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

func (app *App) createTables() error {
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
		id SERIAL PRIMARY KEY,
		team_name VARCHAR(255) NOT NULL,
		redeemed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		staff_pass_id VARCHAR(255),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_staff_mappings_staff_pass_id ON staff_mappings(staff_pass_id);
	CREATE INDEX IF NOT EXISTS idx_staff_mappings_team_name ON staff_mappings(team_name);
	CREATE INDEX IF NOT EXISTS idx_staff_mappings_created_at ON staff_mappings(created_at);
	CREATE INDEX IF NOT EXISTS idx_redemptions_team_name ON redemptions(team_name);
	CREATE INDEX IF NOT EXISTS idx_redemptions_redeemed_at ON redemptions(redeemed_at);
	CREATE INDEX IF NOT EXISTS idx_redemptions_staff_pass_id ON redemptions(staff_pass_id);
	`

	_, err := app.DB.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("error creating tables: %v", err)
	}

	log.Println("Database tables created successfully")
	return nil
}

func (app *App) loadCSVData() error {
	return app.loadStaffMappingsFromPath("/app/data/staff_mappings.csv")
}

func (app *App) loadStaffMappingsFromPath(csvFilePath string) error {
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
		if err := app.insertStaffMapping(mapping); err != nil {
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

func (app *App) insertStaffMapping(mapping StaffMapping) error {
	_, err := app.DB.Exec(`
		INSERT INTO staff_mappings (staff_pass_id, team_name, created_at) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (staff_pass_id, created_at) DO NOTHING`,
		mapping.StaffPassID, mapping.TeamName, mapping.CreatedAt)
	return err
}

func setupRoutes(app *App) *gin.Engine {
	router := gin.Default()

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"service": "govtech-christmas-redemption",
		})
	})

	// API routes
	api := router.Group("/api/v1")
	{
		// Redemption endpoints
		api.GET("/redemptions", app.getRedemptions)
		api.GET("/redemptions/:id", app.getRedemption)
		api.POST("/redemptions", app.createRedemption)
		api.PUT("/redemptions/:id", app.updateRedemption)
		api.DELETE("/redemptions/:id", app.deleteRedemption)

		// Staff mapping endpoints
		api.GET("/staff-mappings", app.getStaffMappings)
		api.GET("/staff-mappings/:staff_pass_id", app.getStaffMapping)

		// Eligibility and lookup endpoints
		api.GET("/lookup/:staff_pass_id", app.lookupStaffPass)
		api.GET("/eligibility/:staff_pass_id", app.checkEligibility)
		api.POST("/redeem/:staff_pass_id", app.redeemForStaff)
	}

	return router
}

func (app *App) getRedemptions(c *gin.Context) {
	rows, err := app.DB.Query("SELECT id, team_name, redeemed_at, staff_pass_id, created_at, updated_at FROM redemptions")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch redemptions"})
		return
	}
	defer rows.Close()

	var redemptions []Redemption
	for rows.Next() {
		var r Redemption
		err := rows.Scan(&r.ID, &r.TeamName, &r.RedeemedAt, &r.StaffPassID, &r.CreatedAt, &r.UpdatedAt)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		redemptions = append(redemptions, r)
	}

	c.JSON(http.StatusOK, redemptions)
}

func (app *App) getRedemption(c *gin.Context) {
	id := c.Param("id")
	
	var r Redemption
	err := app.DB.QueryRow("SELECT id, team_name, redeemed_at, staff_pass_id, created_at, updated_at FROM redemptions WHERE id = $1", id).
		Scan(&r.ID, &r.TeamName, &r.RedeemedAt, &r.StaffPassID, &r.CreatedAt, &r.UpdatedAt)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Redemption not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch redemption"})
		return
	}

	c.JSON(http.StatusOK, r)
}

func (app *App) createRedemption(c *gin.Context) {
	var r Redemption
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := app.DB.QueryRow(`
		INSERT INTO redemptions (team_name, staff_pass_id) 
		VALUES ($1, $2) RETURNING id, redeemed_at, created_at, updated_at`,
		r.TeamName, r.StaffPassID).Scan(&r.ID, &r.RedeemedAt, &r.CreatedAt, &r.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create redemption"})
		return
	}

	c.JSON(http.StatusCreated, r)
}

func (app *App) updateRedemption(c *gin.Context) {
	id := c.Param("id")
	var r Redemption
	
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := app.DB.Exec(`
		UPDATE redemptions 
		SET team_name = $1, staff_pass_id = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3`,
		r.TeamName, r.StaffPassID, id)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update redemption"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Redemption updated successfully"})
}

func (app *App) deleteRedemption(c *gin.Context) {
	id := c.Param("id")
	
	result, err := app.DB.Exec("DELETE FROM redemptions WHERE id = $1", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete redemption"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Redemption not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Redemption deleted successfully"})
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Staff mapping endpoints
func (app *App) getStaffMappings(c *gin.Context) {
	rows, err := app.DB.Query("SELECT id, staff_pass_id, team_name, created_at FROM staff_mappings ORDER BY created_at DESC")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch staff mappings"})
		return
	}
	defer rows.Close()

	var mappings []StaffMapping
	for rows.Next() {
		var m StaffMapping
		err := rows.Scan(&m.ID, &m.StaffPassID, &m.TeamName, &m.CreatedAt)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		mappings = append(mappings, m)
	}

	c.JSON(http.StatusOK, mappings)
}

func (app *App) getStaffMapping(c *gin.Context) {
	staffPassID := c.Param("staff_pass_id")
	
	// Get the latest mapping for this staff pass ID
	var m StaffMapping
	err := app.DB.QueryRow(`
		SELECT id, staff_pass_id, team_name, created_at 
		FROM staff_mappings 
		WHERE staff_pass_id = $1 
		ORDER BY created_at DESC 
		LIMIT 1`, staffPassID).Scan(&m.ID, &m.StaffPassID, &m.TeamName, &m.CreatedAt)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Staff mapping not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch staff mapping"})
		return
	}

	c.JSON(http.StatusOK, m)
}

// Lookup staff pass ID to get team information
func (app *App) lookupStaffPass(c *gin.Context) {
	staffPassID := c.Param("staff_pass_id")
	
	// Get the latest mapping for this staff pass ID
	var m StaffMapping
	err := app.DB.QueryRow(`
		SELECT id, staff_pass_id, team_name, created_at 
		FROM staff_mappings 
		WHERE staff_pass_id = $1 
		ORDER BY created_at DESC 
		LIMIT 1`, staffPassID).Scan(&m.ID, &m.StaffPassID, &m.TeamName, &m.CreatedAt)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Invalid staff pass ID",
			"staff_pass_id": staffPassID,
			"valid": false,
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to lookup staff pass"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"staff_pass_id": m.StaffPassID,
		"team_name": m.TeamName,
		"valid": true,
		"mapping_created_at": m.CreatedAt,
	})
}

// Check eligibility for redemption
func (app *App) checkEligibility(c *gin.Context) {
	staffPassID := c.Param("staff_pass_id")
	
	// First, check if staff pass ID is valid and get team name
	var teamName string
	err := app.DB.QueryRow(`
		SELECT team_name 
		FROM staff_mappings 
		WHERE staff_pass_id = $1 
		ORDER BY created_at DESC 
		LIMIT 1`, staffPassID).Scan(&teamName)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{
			"staff_pass_id": staffPassID,
			"eligible": false,
			"reason": "Invalid staff pass ID",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check eligibility"})
		return
	}

	// Check if team has already redeemed
	var redemptionCount int
	err = app.DB.QueryRow("SELECT COUNT(*) FROM redemptions WHERE team_name = $1", teamName).Scan(&redemptionCount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check team redemption status"})
		return
	}

	if redemptionCount > 0 {
		c.JSON(http.StatusOK, gin.H{
			"staff_pass_id": staffPassID,
			"team_name": teamName,
			"eligible": false,
			"reason": "Team has already redeemed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"staff_pass_id": staffPassID,
		"team_name": teamName,
		"eligible": true,
		"reason": "Team is eligible for redemption",
	})
}

// Redeem for a specific staff member
func (app *App) redeemForStaff(c *gin.Context) {
	staffPassID := c.Param("staff_pass_id")
	
	// Check eligibility first
	var teamName string
	err := app.DB.QueryRow(`
		SELECT team_name 
		FROM staff_mappings 
		WHERE staff_pass_id = $1 
		ORDER BY created_at DESC 
		LIMIT 1`, staffPassID).Scan(&teamName)
	
	if err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid staff pass ID",
			"staff_pass_id": staffPassID,
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate staff pass"})
		return
	}

	// Check if team has already redeemed
	var redemptionCount int
	err = app.DB.QueryRow("SELECT COUNT(*) FROM redemptions WHERE team_name = $1", teamName).Scan(&redemptionCount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check team redemption status"})
		return
	}

	if redemptionCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"error": "Team has already redeemed",
			"team_name": teamName,
		})
		return
	}

	// Create redemption record
	var r Redemption
	err = app.DB.QueryRow(`
		INSERT INTO redemptions (team_name, staff_pass_id) 
		VALUES ($1, $2) 
		RETURNING id, team_name, redeemed_at, staff_pass_id, created_at, updated_at`,
		teamName, staffPassID).Scan(&r.ID, &r.TeamName, &r.RedeemedAt, &r.StaffPassID, &r.CreatedAt, &r.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create redemption record"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Redemption successful",
		"redemption": r,
	})
}