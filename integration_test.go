//go:build integration

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hanju/govtech-christmas/api"
	"github.com/hanju/govtech-christmas/api/cache"
	_ "github.com/lib/pq"
	goredis "github.com/redis/go-redis/v9"
)

// ── Test infrastructure ────────────────────────────────────────────────────

// integrationSetup connects to real PostgreSQL + Redis, cleans state,
// creates tables, loads CSV data, pre-warms cache, and returns a ready App.
// It uses t.Cleanup to tear down at the end of each test.
func integrationSetup(t *testing.T) *api.App {
	t.Helper()

	// Connect to PostgreSQL (defaults match docker-compose.yml)
	dbHost := getEnvOrDefault("DB_HOST", "localhost")
	dbPort := getEnvOrDefault("DB_PORT", "5432")
	dbName := getEnvOrDefault("DB_NAME", "govtech_christmas")
	dbUser := getEnvOrDefault("DB_USER", "postgres")
	dbPass := getEnvOrDefault("DB_PASSWORD", "password123")

	psqlInfo := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		dbHost, dbPort, dbName, dbUser, dbPass)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		t.Fatalf("failed to open PostgreSQL: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("PostgreSQL not available, skipping integration tests: %v", err)
	}

	// Connect to Redis
	redisHost := getEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := getEnvOrDefault("REDIS_PORT", "6379")
	redisClient := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		db.Close()
		t.Skipf("Redis not available, skipping integration tests: %v", err)
	}

	// Clean slate: drop and recreate tables, flush Redis
	cleanDB(t, db)
	redisClient.FlushDB(context.Background())

	// Recreate schema
	if err := createTables(db); err != nil {
		t.Fatalf("createTables failed: %v", err)
	}

	// Load CSV data from the real data/ directory
	if err := loadCSVData(db); err != nil {
		t.Fatalf("loadCSVData failed: %v", err)
	}

	// Pre-warm cache
	redisCache := cache.NewRedisCache(redisClient)
	if err := prewarmCache(db, redisCache); err != nil {
		t.Fatalf("prewarmCache failed: %v", err)
	}

	app := &api.App{DB: db, Cache: redisCache}

	t.Cleanup(func() {
		cleanDB(t, db)
		redisClient.FlushDB(context.Background())
		redisClient.Close()
		db.Close()
	})

	return app
}

// cleanDB drops the tables so each test starts fresh.
func cleanDB(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec("DROP TABLE IF EXISTS redemptions, staff_mappings CASCADE")
	if err != nil {
		t.Fatalf("failed to clean DB: %v", err)
	}
}

// jsonBody is a helper to parse JSON response bodies.
func jsonBody(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON body %q: %v", string(body), err)
	}
	return result
}

func jsonArray(t *testing.T, body []byte) []map[string]interface{} {
	t.Helper()
	var result []map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON array %q: %v", string(body), err)
	}
	return result
}

// ── Health Check ───────────────────────────────────────────────────────────

func TestIntegration_HealthCheck(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := jsonBody(t, w.Body.Bytes())
	if body["status"] != "healthy" {
		t.Errorf("expected status=healthy, got %v", body["status"])
	}
	if body["database"] != true {
		t.Errorf("expected database=true, got %v", body["database"])
	}
	if body["cache"] != true {
		t.Errorf("expected cache=true, got %v", body["cache"])
	}
}

// ── Staff Mappings ─────────────────────────────────────────────────────────

func TestIntegration_StaffMappings(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	// List all staff mappings — CSV has 15 rows
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/staff-mappings", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	mappings := jsonArray(t, w.Body.Bytes())
	if len(mappings) < 15 {
		t.Errorf("expected >= 15 staff mappings from CSV, got %d", len(mappings))
	}

	// Get specific staff mapping
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/staff-mappings/STAFF001", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	m := jsonBody(t, w.Body.Bytes())
	if m["staff_pass_id"] != "STAFF001" {
		t.Errorf("expected staff_pass_id=STAFF001, got %v", m["staff_pass_id"])
	}

	// Not-found staff mapping
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/staff-mappings/NONEXISTENT", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown staff, got %d", w.Code)
	}
}

// ── Lookup ─────────────────────────────────────────────────────────────────

func TestIntegration_Lookup(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	// Valid staff pass
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/lookup/STAFF003", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := jsonBody(t, w.Body.Bytes())
	if body["valid"] != true {
		t.Errorf("expected valid=true, got %v", body["valid"])
	}
	if body["team_name"] != "Team Gamma" {
		t.Errorf("expected team_name=Team Gamma, got %v", body["team_name"])
	}

	// Invalid staff pass
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/lookup/NONEXISTENT", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	body = jsonBody(t, w.Body.Bytes())
	if body["valid"] != false {
		t.Errorf("expected valid=false, got %v", body["valid"])
	}
}

// ── Cache Prewarm Verification ─────────────────────────────────────────────

func TestIntegration_CachePrewarm(t *testing.T) {
	app := integrationSetup(t)

	// After prewarm, Redis should have staff mappings cached.
	// STAFF010 maps to Team Alpha (latest by created_at for that staff_pass_id).
	ctx := context.Background()
	team, found, err := app.Cache.GetStaffTeam(ctx, "STAFF010")
	if err != nil {
		t.Fatalf("GetStaffTeam error: %v", err)
	}
	if !found {
		t.Fatal("expected STAFF010 to be in cache after prewarm")
	}
	if team != "Team Alpha" {
		t.Errorf("expected Team Alpha, got %q", team)
	}

	// STAFF007 → Team Epsilon
	team, found, err = app.Cache.GetStaffTeam(ctx, "STAFF007")
	if err != nil {
		t.Fatalf("GetStaffTeam error: %v", err)
	}
	if !found || team != "Team Epsilon" {
		t.Errorf("expected STAFF007 → Team Epsilon in cache, got found=%v team=%q", found, team)
	}
}

// ── Eligibility ────────────────────────────────────────────────────────────

func TestIntegration_Eligibility(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	// Team Gamma has no redemption → eligible
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/eligibility/STAFF003", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := jsonBody(t, w.Body.Bytes())
	if body["eligible"] != true {
		t.Errorf("expected eligible=true for unredeemed team, got %v", body["eligible"])
	}

	// Invalid staff
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/eligibility/NONEXISTENT", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body = jsonBody(t, w.Body.Bytes())
	if body["eligible"] != false {
		t.Errorf("expected eligible=false for invalid staff, got %v", body["eligible"])
	}
}

// ── Full Redemption Round-Trip ─────────────────────────────────────────────

func TestIntegration_RedemptionRoundTrip(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	// Step 1: Verify Team Gamma is eligible
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/eligibility/STAFF003", nil)
	router.ServeHTTP(w, req)

	body := jsonBody(t, w.Body.Bytes())
	if body["eligible"] != true {
		t.Fatalf("expected Team Gamma eligible before redemption, got %v", body["eligible"])
	}

	// Step 2: Redeem for STAFF003 (Team Gamma)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/redeem/STAFF003", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on first redeem, got %d: %s", w.Code, w.Body.String())
	}
	body = jsonBody(t, w.Body.Bytes())
	if body["message"] != "Redemption successful" {
		t.Errorf("expected 'Redemption successful', got %v", body["message"])
	}

	// Step 3: Verify Team Gamma is no longer eligible
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/eligibility/STAFF003", nil)
	router.ServeHTTP(w, req)

	body = jsonBody(t, w.Body.Bytes())
	if body["eligible"] != false {
		t.Errorf("expected eligible=false after redemption, got %v", body["eligible"])
	}

	// Step 4: Second redemption attempt → 409 Conflict
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/redeem/STAFF003", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate redeem, got %d", w.Code)
	}

	// Step 5: Another staff on the same team (STAFF008 → Team Gamma) also blocked
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/redeem/STAFF008", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for same-team different-staff, got %d: %s", w.Code, w.Body.String())
	}

	// Step 6: Verify redemption appears in the list
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/redemptions/Team Gamma", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body = jsonBody(t, w.Body.Bytes())
	if body["redeemed"] != true {
		t.Errorf("expected redeemed=true in DB, got %v", body["redeemed"])
	}

	// Step 7: Delete redemption → team can redeem again
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/api/v1/redemptions/Team Gamma", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d", w.Code)
	}

	// Step 8: Verify team is eligible again after delete
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/eligibility/STAFF003", nil)
	router.ServeHTTP(w, req)

	body = jsonBody(t, w.Body.Bytes())
	if body["eligible"] != true {
		t.Errorf("expected eligible=true after delete, got %v", body["eligible"])
	}

	// Step 9: Re-redeem succeeds
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/redeem/STAFF003", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 on re-redeem, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Redemption CRUD ────────────────────────────────────────────────────────

func TestIntegration_RedemptionCRUD(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	// Create
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/redemptions",
		strings.NewReader(`{"team_name":"TestTeamCRUD","redeemed":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Read
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/redemptions/TestTeamCRUD", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("read: expected 200, got %d", w.Code)
	}
	body := jsonBody(t, w.Body.Bytes())
	if body["redeemed"] != false {
		t.Errorf("expected redeemed=false after create, got %v", body["redeemed"])
	}

	// Update
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/api/v1/redemptions/TestTeamCRUD",
		strings.NewReader(`{"redeemed":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify update
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/redemptions/TestTeamCRUD", nil)
	router.ServeHTTP(w, req)
	body = jsonBody(t, w.Body.Bytes())
	if body["redeemed"] != true {
		t.Errorf("expected redeemed=true after update, got %v", body["redeemed"])
	}

	// Delete
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/api/v1/redemptions/TestTeamCRUD", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", w.Code)
	}

	// Verify gone
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/redemptions/TestTeamCRUD", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

// ── Concurrent Redemption with Real Redis SETNX ────────────────────────────

func TestIntegration_ConcurrentRedemption(t *testing.T) {
	app := integrationSetup(t)

	// STAFF005 → Team Delta (from CSV), no prior redemption
	const goroutines = 20

	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		successes  int
		rejections int
		errs       int
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			result, err := app.RedeemPresent("STAFF005")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs++
				t.Logf("RedeemPresent error: %v", err)
				return
			}
			if result.Success {
				successes++
			} else {
				rejections++
			}
		}()
	}

	wg.Wait()

	if errs != 0 {
		t.Errorf("expected 0 errors, got %d", errs)
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
	if rejections != goroutines-1 {
		t.Errorf("expected %d rejections, got %d", goroutines-1, rejections)
	}

	// Verify DB has exactly one redemption
	var count int
	err := app.DB.QueryRow("SELECT COUNT(*) FROM redemptions WHERE team_name = 'Team Delta' AND redeemed = TRUE").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query DB: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 redemption row in DB, got %d", count)
	}

	// Verify Redis has the redemption locked
	ctx := context.Background()
	redeemed, found, err := app.Cache.GetRedemptionStatus(ctx, "Team Delta")
	if err != nil {
		t.Fatalf("GetRedemptionStatus error: %v", err)
	}
	if !found || !redeemed {
		t.Errorf("expected redemption locked in Redis, got found=%v redeemed=%v", found, redeemed)
	}
}

// ── Redeem Invalid Staff Pass ──────────────────────────────────────────────

func TestIntegration_RedeemInvalidStaff(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/redeem/NONEXISTENT", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid staff, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Cache-Aside Verification ───────────────────────────────────────────────

func TestIntegration_CacheAsidePopulation(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	// Flush Redis to clear prewarm data, forcing cache misses
	redisHost := getEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := getEnvOrDefault("REDIS_PORT", "6379")
	tempClient := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})
	tempClient.FlushDB(ctx)
	tempClient.Close()

	// Verify cache is empty for STAFF003
	_, found, _ := app.Cache.GetStaffTeam(ctx, "STAFF003")
	if found {
		t.Fatal("expected cache miss after flush")
	}

	// Call CheckEligibility — should populate cache via cache-aside
	eligible, _, err := app.CheckEligibility("STAFF003")
	if err != nil {
		t.Fatalf("CheckEligibility error: %v", err)
	}
	if !eligible {
		t.Error("expected eligible=true")
	}

	// Now cache should be populated
	team, found, err := app.Cache.GetStaffTeam(ctx, "STAFF003")
	if err != nil {
		t.Fatalf("GetStaffTeam error: %v", err)
	}
	if !found {
		t.Error("expected cache hit after cache-aside lookup")
	}
	if team != "Team Gamma" {
		t.Errorf("expected Team Gamma, got %q", team)
	}
}

// ── Load Staff Mappings Tests ──────────────────────────────────────────────

func TestIntegration_LoadStaffMappingsFromPath_FileNotFound(t *testing.T) {
	app := integrationSetup(t)

	err := loadStaffMappingsFromPath(app.DB, "/nonexistent/path/staff_mappings.csv")
	if err != nil {
		t.Errorf("expected nil error for missing file, got: %v", err)
	}
}

func TestIntegration_LoadStaffMappingsFromPath_ValidCSV(t *testing.T) {
	app := integrationSetup(t)

	content := "staff_pass_id,team_name,created_at\nINTTEST001,IntTestTeam,1700000000000\nINTTEST002,IntTestTeam2,1700000001000\n"
	f, err := os.CreateTemp("", "staff_mappings_inttest_*.csv")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	if err := loadStaffMappingsFromPath(app.DB, f.Name()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify rows exist in real DB
	var count int
	err = app.DB.QueryRow("SELECT COUNT(*) FROM staff_mappings WHERE staff_pass_id IN ('INTTEST001', 'INTTEST002')").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query DB: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows inserted, got %d", count)
	}
}

func TestIntegration_LoadStaffMappingsFromPath_EmptyFile(t *testing.T) {
	app := integrationSetup(t)

	f, err := os.CreateTemp("", "staff_mappings_empty_*.csv")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.Close()

	err = loadStaffMappingsFromPath(app.DB, f.Name())
	if err == nil {
		t.Error("expected error for empty file, got nil")
	}
}

// ── Route Registration ─────────────────────────────────────────────────────

func TestIntegration_RoutesExist(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/api/v1/redemptions"},
		{http.MethodGet, "/api/v1/staff-mappings"},
		{http.MethodGet, "/api/v1/lookup/STAFF001"},
		{http.MethodGet, "/api/v1/eligibility/STAFF001"},
	}

	for _, rt := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(rt.method, rt.path, nil)
		router.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s %s returned 404 — route not registered", rt.method, rt.path)
		}
	}
}

// ── Service-Level Redemption Tests ─────────────────────────────────────────

func TestIntegration_RedeemPresent_ServiceLevel(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	// STAFF007 → Team Epsilon (from CSV), no prior redemption
	result, err := app.RedeemPresent("STAFF007")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got %v", result.Success)
	}
	if result.TeamName != "Team Epsilon" {
		t.Errorf("expected team_name='Team Epsilon', got %v", result.TeamName)
	}
	if result.Redemption == nil {
		t.Fatal("expected redemption record, got nil")
	}
	if !result.Redemption.Redeemed {
		t.Error("expected redemption.Redeemed=true")
	}
	if !strings.Contains(result.Message, "Successfully redeemed") {
		t.Errorf("expected success message, got: %s", result.Message)
	}

	// Verify cache was populated
	team, found, _ := app.Cache.GetStaffTeam(ctx, "STAFF007")
	if !found || team != "Team Epsilon" {
		t.Errorf("expected staff team in cache, got found=%v team=%q", found, team)
	}
	redeemed, found, _ := app.Cache.GetRedemptionStatus(ctx, "Team Epsilon")
	if !found || !redeemed {
		t.Errorf("expected redemption locked in cache, got found=%v redeemed=%v", found, redeemed)
	}
}

func TestIntegration_RedeemPresent_InvalidStaff_ServiceLevel(t *testing.T) {
	app := integrationSetup(t)

	result, err := app.RedeemPresent("INVALID001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("expected success=false, got %v", result.Success)
	}
	if !strings.Contains(result.Message, "Invalid staff pass ID") {
		t.Errorf("expected invalid staff pass message, got: %s", result.Message)
	}
	if result.Redemption != nil {
		t.Errorf("expected nil redemption, got non-nil")
	}
}

func TestIntegration_RedeemPresent_AlreadyRedeemed_ServiceLevel(t *testing.T) {
	app := integrationSetup(t)

	// STAFF011 → Team Zeta — first redeem succeeds
	result, err := app.RedeemPresent("STAFF011")
	if err != nil || !result.Success {
		t.Fatalf("first redeem should succeed: err=%v", err)
	}

	// Second attempt should be rejected
	result, err = app.RedeemPresent("STAFF011")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("expected success=false, got %v", result.Success)
	}
	if result.TeamName != "Team Zeta" {
		t.Errorf("expected team_name='Team Zeta', got %v", result.TeamName)
	}
	if !strings.Contains(result.Message, "already redeemed") {
		t.Errorf("expected already redeemed message, got: %s", result.Message)
	}
	if result.Redemption != nil {
		t.Errorf("expected nil redemption, got non-nil")
	}
}

// ── Service-Level Eligibility Tests ────────────────────────────────────────

func TestIntegration_CheckEligibility_Eligible_ServiceLevel(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	// STAFF015 → Team Eta (from CSV), no prior redemption
	eligible, reason, err := app.CheckEligibility("STAFF015")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eligible {
		t.Errorf("expected eligible=true, got %v", eligible)
	}
	if reason != "Team 'Team Eta' is eligible for redemption" {
		t.Errorf("unexpected reason: %s", reason)
	}

	// Verify staff team was cached
	team, found, _ := app.Cache.GetStaffTeam(ctx, "STAFF015")
	if !found || team != "Team Eta" {
		t.Errorf("expected staff team cached, got found=%v team=%q", found, team)
	}
}

func TestIntegration_CheckEligibility_InvalidStaff_ServiceLevel(t *testing.T) {
	app := integrationSetup(t)

	eligible, reason, err := app.CheckEligibility("INVALID001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Errorf("expected eligible=false, got %v", eligible)
	}
	if reason != "Invalid staff pass ID" {
		t.Errorf("expected 'Invalid staff pass ID', got: %s", reason)
	}
}

func TestIntegration_CheckEligibility_AlreadyRedeemed_ServiceLevel(t *testing.T) {
	app := integrationSetup(t)

	// First redeem Team Eta via STAFF015
	result, err := app.RedeemPresent("STAFF015")
	if err != nil || !result.Success {
		t.Fatalf("setup redeem should succeed: err=%v", err)
	}

	// Now check eligibility — should report already redeemed
	eligible, reason, err := app.CheckEligibility("STAFF015")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Errorf("expected eligible=false after redemption, got %v", eligible)
	}
	if !strings.Contains(reason, "already redeemed") {
		t.Errorf("expected already redeemed reason, got: %s", reason)
	}
}

// ── Handler Edge Cases ─────────────────────────────────────────────────────

func TestIntegration_Handler_InvalidJSON(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	// Invalid JSON on POST /redemptions
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/redemptions", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("create with bad JSON: expected 400, got %d", w.Code)
	}

	// Create a valid redemption for update test
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/redemptions",
		strings.NewReader(`{"team_name":"TestBadJSON","redeemed":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d", w.Code)
	}

	// Invalid JSON on PUT /redemptions
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPut, "/api/v1/redemptions/TestBadJSON", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("update with bad JSON: expected 400, got %d", w.Code)
	}
}

func TestIntegration_Handler_UpdateRedemption_NotFound(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/redemptions/NonexistentTeam",
		strings.NewReader(`{"redeemed":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestIntegration_Handler_DeleteRedemption_NotFound(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/v1/redemptions/NonexistentTeam", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestIntegration_Handler_DeleteCacheInvalidation(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)
	ctx := context.Background()

	// Create a redemption via the redeem endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/redeem/STAFF003", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup redeem: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify cache has the redemption
	redeemed, found, _ := app.Cache.GetRedemptionStatus(ctx, "Team Gamma")
	if !found || !redeemed {
		t.Fatalf("expected redemption in cache, got found=%v redeemed=%v", found, redeemed)
	}

	// Delete via API
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/api/v1/redemptions/Team Gamma", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", w.Code)
	}

	// Verify cache was invalidated
	_, found, _ = app.Cache.GetRedemptionStatus(ctx, "Team Gamma")
	if found {
		t.Error("expected cache invalidated after DELETE, but redemption still in cache")
	}
}

func TestIntegration_Handler_WriteThroughCache(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	// Flush Redis to clear prewarm data
	redisHost := getEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := getEnvOrDefault("REDIS_PORT", "6379")
	tempClient := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})
	tempClient.FlushDB(ctx)
	tempClient.Close()

	// Verify cache is empty
	_, found, _ := app.Cache.GetStaffTeam(ctx, "STAFF001")
	if found {
		t.Fatal("expected cache miss after flush")
	}

	// Hit /staff-mappings/:id endpoint
	router := api.SetupRoutes(app)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/staff-mappings/STAFF001", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify write-through cache population
	team, found, _ := app.Cache.GetStaffTeam(ctx, "STAFF001")
	if !found {
		t.Error("expected cache populated after staff mapping retrieval")
	}
	if team != "Team Alpha" {
		t.Errorf("expected Team Alpha in cache, got %q", team)
	}

	// Flush again and test lookup endpoint
	tempClient = goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})
	tempClient.FlushDB(ctx)
	tempClient.Close()

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/lookup/STAFF003", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	team, found, _ = app.Cache.GetStaffTeam(ctx, "STAFF003")
	if !found || team != "Team Gamma" {
		t.Errorf("expected Team Gamma in cache after lookup, got found=%v team=%q", found, team)
	}
}

func TestIntegration_Handler_GetRedemptions_List(t *testing.T) {
	app := integrationSetup(t)
	router := api.SetupRoutes(app)

	// Create two redemptions
	for _, team := range []string{"ListTestA", "ListTestB"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/redemptions",
			strings.NewReader(fmt.Sprintf(`{"team_name":"%s","redeemed":true}`, team)))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("setup create %s: expected 201, got %d", team, w.Code)
		}
	}

	// Get list
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/redemptions", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	list := jsonArray(t, w.Body.Bytes())
	if len(list) < 2 {
		t.Errorf("expected at least 2 redemptions, got %d", len(list))
	}
}

// ── Redis CacheStore Behavioral Tests ──────────────────────────────────────

func TestIntegration_RedisCache_StaffTeam_MissThenHit(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	// Flush to ensure clean state
	redisHost := getEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := getEnvOrDefault("REDIS_PORT", "6379")
	tempClient := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort),
	})
	tempClient.FlushDB(ctx)
	tempClient.Close()

	// Miss
	team, found, err := app.Cache.GetStaffTeam(ctx, "CACHETEST001")
	if err != nil || found || team != "" {
		t.Errorf("expected miss, got found=%v team=%q err=%v", found, team, err)
	}

	// Set and hit
	if err := app.Cache.SetStaffTeam(ctx, "CACHETEST001", "CacheTeam"); err != nil {
		t.Fatalf("SetStaffTeam error: %v", err)
	}
	team, found, err = app.Cache.GetStaffTeam(ctx, "CACHETEST001")
	if err != nil || !found || team != "CacheTeam" {
		t.Errorf("expected hit with CacheTeam, got found=%v team=%q err=%v", found, team, err)
	}
}

func TestIntegration_RedisCache_KeyIsolation(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	_ = app.Cache.SetStaffTeam(ctx, "ISO001", "TeamA")
	_ = app.Cache.SetStaffTeam(ctx, "ISO002", "TeamB")

	if team, _, _ := app.Cache.GetStaffTeam(ctx, "ISO001"); team != "TeamA" {
		t.Errorf("expected TeamA, got %q", team)
	}
	if team, _, _ := app.Cache.GetStaffTeam(ctx, "ISO002"); team != "TeamB" {
		t.Errorf("expected TeamB, got %q", team)
	}
}

func TestIntegration_RedisCache_RedemptionNX_WinAndLose(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	// Miss
	redeemed, found, err := app.Cache.GetRedemptionStatus(ctx, "NXTeam")
	if err != nil || found || redeemed {
		t.Errorf("expected miss, got found=%v redeemed=%v err=%v", found, redeemed, err)
	}

	// First SETNX wins
	set, err := app.Cache.SetRedemptionNX(ctx, "NXTeam")
	if err != nil || !set {
		t.Fatalf("expected SETNX win, got set=%v err=%v", set, err)
	}

	// Now cached as redeemed
	redeemed, found, err = app.Cache.GetRedemptionStatus(ctx, "NXTeam")
	if err != nil || !found || !redeemed {
		t.Errorf("expected hit redeemed=true, got found=%v redeemed=%v err=%v", found, redeemed, err)
	}

	// Second SETNX loses
	set, err = app.Cache.SetRedemptionNX(ctx, "NXTeam")
	if err != nil || set {
		t.Errorf("expected SETNX lose, got set=%v err=%v", set, err)
	}
}

func TestIntegration_RedisCache_InvalidateRedemption(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	_, _ = app.Cache.SetRedemptionNX(ctx, "InvTeam")

	if err := app.Cache.InvalidateRedemption(ctx, "InvTeam"); err != nil {
		t.Fatalf("InvalidateRedemption error: %v", err)
	}

	// Miss after invalidation
	_, found, err := app.Cache.GetRedemptionStatus(ctx, "InvTeam")
	if err != nil || found {
		t.Errorf("expected miss after invalidation, got found=%v err=%v", found, err)
	}

	// Re-NX should succeed
	set, err := app.Cache.SetRedemptionNX(ctx, "InvTeam")
	if err != nil || !set {
		t.Errorf("expected SETNX win after invalidation, got set=%v err=%v", set, err)
	}
}

func TestIntegration_RedisCache_InvalidateNonexistent(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	if err := app.Cache.InvalidateRedemption(ctx, "NoSuchTeam"); err != nil {
		t.Errorf("expected no error invalidating missing key, got: %v", err)
	}
}

func TestIntegration_RedisCache_Ping(t *testing.T) {
	app := integrationSetup(t)
	if err := app.Cache.Ping(context.Background()); err != nil {
		t.Errorf("expected successful Ping, got: %v", err)
	}
}

func TestIntegration_RedisCache_ConcurrentNX(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			set, err := app.Cache.SetRedemptionNX(ctx, "ConcurrentTeam")
			if err != nil {
				t.Errorf("SetRedemptionNX error: %v", err)
				return
			}
			if set {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if winners != 1 {
		t.Errorf("expected exactly 1 SETNX winner across %d goroutines, got %d", goroutines, winners)
	}
}

func TestIntegration_RedisCache_ConcurrentStaffTeam(t *testing.T) {
	app := integrationSetup(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = app.Cache.SetStaffTeam(ctx, "CONCURRENT001", "TeamParallel")
		}()
	}
	wg.Wait()

	team, found, err := app.Cache.GetStaffTeam(ctx, "CONCURRENT001")
	if err != nil || !found || team != "TeamParallel" {
		t.Errorf("expected TeamParallel after concurrent writes, got found=%v team=%q err=%v", found, team, err)
	}
}
