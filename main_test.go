package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hanju/govtech-christmas/api"
	"github.com/hanju/govtech-christmas/api/cache"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// ── parseStaffMappingsCSV ──────────────────────────────────────────────────

func TestParseStaffMappingsCSV_ValidData(t *testing.T) {
	input := `staff_pass_id,team_name,created_at
STAFF001,Team Alpha,1703462400000
STAFF002,Team Beta,1703548800000
STAFF003,Team Gamma,1703635200000`

	mappings, err := parseStaffMappingsCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mappings) != 3 {
		t.Fatalf("expected 3 mappings, got %d", len(mappings))
	}

	cases := []StaffMapping{
		{StaffPassID: "STAFF001", TeamName: "Team Alpha", CreatedAt: 1703462400000},
		{StaffPassID: "STAFF002", TeamName: "Team Beta", CreatedAt: 1703548800000},
		{StaffPassID: "STAFF003", TeamName: "Team Gamma", CreatedAt: 1703635200000},
	}
	for i, want := range cases {
		got := mappings[i]
		if got.StaffPassID != want.StaffPassID {
			t.Errorf("row %d: StaffPassID = %q, want %q", i, got.StaffPassID, want.StaffPassID)
		}
		if got.TeamName != want.TeamName {
			t.Errorf("row %d: TeamName = %q, want %q", i, got.TeamName, want.TeamName)
		}
		if got.CreatedAt != want.CreatedAt {
			t.Errorf("row %d: CreatedAt = %d, want %d", i, got.CreatedAt, want.CreatedAt)
		}
	}
}

func TestParseStaffMappingsCSV_HeaderOnly(t *testing.T) {
	input := `staff_pass_id,team_name,created_at`

	mappings, err := parseStaffMappingsCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mappings) != 0 {
		t.Fatalf("expected 0 mappings for header-only file, got %d", len(mappings))
	}
}

func TestParseStaffMappingsCSV_EmptyFile(t *testing.T) {
	_, err := parseStaffMappingsCSV(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected an error for empty file, got nil")
	}
}

func TestParseStaffMappingsCSV_InvalidTimestamp(t *testing.T) {
	input := `staff_pass_id,team_name,created_at
STAFF001,Team Alpha,not-a-number
STAFF002,Team Beta,1703548800000`

	mappings, err := parseStaffMappingsCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Row with invalid timestamp should be skipped; only second row is valid
	if len(mappings) != 1 {
		t.Fatalf("expected 1 valid mapping, got %d", len(mappings))
	}
	if mappings[0].StaffPassID != "STAFF002" {
		t.Errorf("expected STAFF002, got %q", mappings[0].StaffPassID)
	}
}

func TestParseStaffMappingsCSV_InvalidColumnCount(t *testing.T) {
	// One row has too few columns, one has too many, one is valid
	input := `staff_pass_id,team_name,created_at
STAFF001,Team Alpha
STAFF002,Team Beta,1703548800000,extra_column
STAFF003,Team Gamma,1703635200000`

	mappings, err := parseStaffMappingsCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("expected 1 valid mapping (malformed rows skipped), got %d", len(mappings))
	}
	if mappings[0].StaffPassID != "STAFF003" {
		t.Errorf("expected STAFF003, got %q", mappings[0].StaffPassID)
	}
}

func TestParseStaffMappingsCSV_MixedValidAndInvalid(t *testing.T) {
	input := `staff_pass_id,team_name,created_at
STAFF001,Team Alpha,1703462400000
STAFF002,Team Beta,bad-ts
STAFF003,Team Gamma,1703635200000
STAFF004,Team Delta`

	mappings, err := parseStaffMappingsCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mappings) != 2 {
		t.Fatalf("expected 2 valid mappings, got %d", len(mappings))
	}
}

// ── loadStaffMappingsFromPath ──────────────────────────────────────────────

func TestLoadStaffMappingsFromPath_FileNotFound(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	err = loadStaffMappingsFromPath(db, "/nonexistent/path/staff_mappings.csv")
	if err != nil {
		t.Errorf("expected nil error for missing file, got: %v", err)
	}
}

func TestLoadStaffMappingsFromPath_ValidFile(t *testing.T) {
	// Write a temp CSV
	content := "staff_pass_id,team_name,created_at\nSTAFF001,Team Alpha,1703462400000\nSTAFF002,Team Beta,1703548800000\n"
	f, err := os.CreateTemp("", "staff_mappings_*.csv")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	// Expect two INSERT calls (one per valid row)
	mock.ExpectExec(`INSERT INTO staff_mappings`).
		WithArgs("STAFF001", "Team Alpha", int64(1703462400000)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO staff_mappings`).
		WithArgs("STAFF002", "Team Beta", int64(1703548800000)).
		WillReturnResult(sqlmock.NewResult(2, 1))

	if err := loadStaffMappingsFromPath(db, f.Name()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestLoadStaffMappingsFromPath_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp("", "staff_mappings_empty_*.csv")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	f.Close()

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	err = loadStaffMappingsFromPath(db, f.Name())
	if err == nil {
		t.Error("expected error for empty file, got nil")
	}
}

// ── getEnvOrDefault ────────────────────────────────────────────────────────

func TestGetEnvOrDefault_ReturnsEnvVar(t *testing.T) {
	t.Setenv("TEST_KEY", "test_value")
	got := getEnvOrDefault("TEST_KEY", "default")
	if got != "test_value" {
		t.Errorf("expected %q, got %q", "test_value", got)
	}
}

func TestGetEnvOrDefault_ReturnsDefault(t *testing.T) {
	os.Unsetenv("NONEXISTENT_KEY")
	got := getEnvOrDefault("NONEXISTENT_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("expected %q, got %q", "fallback", got)
	}
}

// ── setupRoutes ────────────────────────────────────────────────────────────

func TestSetupRoutes_HealthCheckResponds(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}
	router := api.SetupRoutes(app)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "healthy") {
		t.Errorf("expected body to contain %q, got: %s", "healthy", body)
	}
}

func TestSetupRoutes_ExpectedRoutesExist(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}
	router := api.SetupRoutes(app)

	// Each route should return something other than 404
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

// ── Present Redemption Tests ───────────────────────────────────────────────

func TestRedeemPresent_SuccessfulRedemption(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnRows(sqlmock.NewRows([]string{"team_name"}).AddRow("Team Alpha"))

	// With MemoryCache, SETNX succeeds (first caller wins) so no SELECT redeemed query.

	// Mock successful redemption insertion
	testTime := time.Date(2026, 3, 7, 14, 30, 45, 0, time.UTC)
	mock.ExpectQuery(`INSERT INTO redemptions \(team_name, redeemed, redeemed_at\) VALUES \(\$1, TRUE, CURRENT_TIMESTAMP\) ON CONFLICT \(team_name\) DO UPDATE SET redeemed = TRUE, redeemed_at = CURRENT_TIMESTAMP RETURNING team_name, redeemed, redeemed_at`).
		WithArgs("Team Alpha").
		WillReturnRows(sqlmock.NewRows([]string{"team_name", "redeemed", "redeemed_at"}).
			AddRow("Team Alpha", true, testTime))

	result, err := app.RedeemPresent("STAFF001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success=true, got %v", result.Success)
	}
	if result.TeamName != "Team Alpha" {
		t.Errorf("expected team_name='Team Alpha', got %v", result.TeamName)
	}
	if result.Redemption == nil {
		t.Errorf("expected redemption record, got nil")
	}
	if !strings.Contains(result.Message, "Successfully redeemed") {
		t.Errorf("expected success message, got: %s", result.Message)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestRedeemPresent_InvalidStaffPassID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query returning no rows
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("INVALID001").
		WillReturnError(sql.ErrNoRows)

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
		t.Errorf("expected nil redemption record, got %v", result.Redemption)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestRedeemPresent_TeamAlreadyRedeemed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mc := cache.NewMemoryCache()
	app := &api.App{DB: db, Cache: mc}

	// Pre-populate cache: another desk already redeemed for this team
	if _, err := mc.SetRedemptionNX(context.Background(), "Team Alpha"); err != nil {
		t.Fatalf("failed to pre-populate cache: %v", err)
	}

	// Mock staff pass validation query
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnRows(sqlmock.NewRows([]string{"team_name"}).AddRow("Team Alpha"))

	// SETNX returns false — no DB redemption query needed

	result, err := app.RedeemPresent("STAFF001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Errorf("expected success=false, got %v", result.Success)
	}
	if result.TeamName != "Team Alpha" {
		t.Errorf("expected team_name='Team Alpha', got %v", result.TeamName)
	}
	if !strings.Contains(result.Message, "already redeemed") {
		t.Errorf("expected already redeemed message, got: %s", result.Message)
	}
	if result.Redemption != nil {
		t.Errorf("expected nil redemption record, got %v", result.Redemption)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestRedeemPresent_DatabaseErrorDuringStaffValidation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query with database error
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnError(fmt.Errorf("database connection lost"))

	result, err := app.RedeemPresent("STAFF001")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result on error, got %v", result)
	}
	if !strings.Contains(err.Error(), "failed to validate staff pass") {
		t.Errorf("expected staff validation error, got: %s", err.Error())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestRedeemPresent_DatabaseErrorDuringRedemptionCreation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnRows(sqlmock.NewRows([]string{"team_name"}).AddRow("Team Alpha"))

	// With MemoryCache, SETNX succeeds so no SELECT redeemed query.

	// Mock failed redemption insertion
	mock.ExpectQuery(`INSERT INTO redemptions \(team_name, redeemed, redeemed_at\) VALUES \(\$1, TRUE, CURRENT_TIMESTAMP\) ON CONFLICT \(team_name\) DO UPDATE SET redeemed = TRUE, redeemed_at = CURRENT_TIMESTAMP RETURNING team_name, redeemed, redeemed_at`).
		WithArgs("Team Alpha").
		WillReturnError(fmt.Errorf("database write failed"))

	result, err := app.RedeemPresent("STAFF001")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result on error, got %v", result)
	}
	if !strings.Contains(err.Error(), "failed to create redemption record") {
		t.Errorf("expected redemption creation error, got: %s", err.Error())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

// ── Eligibility Check Tests ─────────────────────────────────────────────────

func TestCheckEligibility_ValidStaffPassEligibleTeam(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnRows(sqlmock.NewRows([]string{"team_name"}).AddRow("Team Alpha"))

	// Mock redemption check query returning no rows (not redeemed)
	mock.ExpectQuery(`SELECT redeemed FROM redemptions WHERE team_name = \$1`).
		WithArgs("Team Alpha").
		WillReturnError(sql.ErrNoRows)

	eligible, reason, err := app.CheckEligibility("STAFF001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !eligible {
		t.Errorf("expected eligible=true, got %v", eligible)
	}
	if reason != "Team 'Team Alpha' is eligible for redemption" {
		t.Errorf("expected eligible reason, got: %s", reason)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestCheckEligibility_InvalidStaffPass(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query returning no rows
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("INVALID001").
		WillReturnError(sql.ErrNoRows)

	eligible, reason, err := app.CheckEligibility("INVALID001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if eligible {
		t.Errorf("expected eligible=false, got %v", eligible)
	}
	if reason != "Invalid staff pass ID" {
		t.Errorf("expected invalid staff pass reason, got: %s", reason)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestCheckEligibility_TeamAlreadyRedeemed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnRows(sqlmock.NewRows([]string{"team_name"}).AddRow("Team Alpha"))

	// Mock redemption check query returning already redeemed
	mock.ExpectQuery(`SELECT redeemed FROM redemptions WHERE team_name = \$1`).
		WithArgs("Team Alpha").
		WillReturnRows(sqlmock.NewRows([]string{"redeemed"}).AddRow(true))

	eligible, reason, err := app.CheckEligibility("STAFF001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if eligible {
		t.Errorf("expected eligible=false, got %v", eligible)
	}
	if !strings.Contains(reason, "already redeemed") {
		t.Errorf("expected already redeemed reason, got: %s", reason)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestCheckEligibility_DatabaseErrorDuringStaffValidation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query with database error
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnError(fmt.Errorf("database connection lost"))

	eligible, reason, err := app.CheckEligibility("STAFF001")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if eligible {
		t.Errorf("expected eligible=false on error, got %v", eligible)
	}
	if reason != "" {
		t.Errorf("expected empty reason on error, got: %s", reason)
	}
	if !strings.Contains(err.Error(), "failed to check eligibility") {
		t.Errorf("expected eligibility check error, got: %s", err.Error())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}

func TestCheckEligibility_DatabaseErrorDuringRedemptionCheck(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	app := &api.App{DB: db, Cache: cache.NewMemoryCache()}

	// Mock staff pass validation query
	mock.ExpectQuery(`SELECT team_name FROM staff_mappings WHERE staff_pass_id = \$1 ORDER BY created_at DESC LIMIT 1`).
		WithArgs("STAFF001").
		WillReturnRows(sqlmock.NewRows([]string{"team_name"}).AddRow("Team Alpha"))

	// Mock redemption check query with database error
	mock.ExpectQuery(`SELECT redeemed FROM redemptions WHERE team_name = \$1`).
		WithArgs("Team Alpha").
		WillReturnError(fmt.Errorf("database connection lost"))

	eligible, reason, err := app.CheckEligibility("STAFF001")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if eligible {
		t.Errorf("expected eligible=false on error, got %v", eligible)
	}
	if reason != "" {
		t.Errorf("expected empty reason on error, got: %s", reason)
	}
	if !strings.Contains(err.Error(), "failed to check team redemption status") {
		t.Errorf("expected team redemption status error, got: %s", err.Error())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectation: %v", err)
	}
}
