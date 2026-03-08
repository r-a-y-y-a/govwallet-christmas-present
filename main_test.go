package main

import (
	"os"
	"strings"
	"testing"
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
