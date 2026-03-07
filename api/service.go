package api

import (
	"database/sql"
	"fmt"
)

// RedeemResult contains the result of a redemption attempt
type RedeemResult struct {
	Success    bool
	TeamName   string
	Message    string
	Redemption *Redemption
}

// RedeemPresent attempts to redeem a present for a given staff pass ID
func (app *App) RedeemPresent(staffPassID string) (*RedeemResult, error) {
	// Check if staff pass ID is valid and get team name
	var teamName string
	err := app.DB.QueryRow(`
		SELECT team_name 
		FROM staff_mappings 
		WHERE staff_pass_id = $1 
		ORDER BY created_at DESC 
		LIMIT 1`, staffPassID).Scan(&teamName)

	if err == sql.ErrNoRows {
		return &RedeemResult{
			Success: false,
			Message: fmt.Sprintf("Invalid staff pass ID: %s", staffPassID),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to validate staff pass: %w", err)
	}

	// Check if team has already redeemed
	var redeemed bool
	err = app.DB.QueryRow("SELECT redeemed FROM redemptions WHERE team_name = $1", teamName).Scan(&redeemed)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to check team redemption status: %w", err)
	}

	if redeemed {
		return &RedeemResult{
			Success:  false,
			TeamName: teamName,
			Message:  fmt.Sprintf("Team '%s' has already redeemed their present", teamName),
		}, nil
	}

	// Upsert redemption record, marking as redeemed
	var r Redemption
	err = app.DB.QueryRow(`
		INSERT INTO redemptions (team_name, redeemed, redeemed_at)
		VALUES ($1, TRUE, CURRENT_TIMESTAMP)
		ON CONFLICT (team_name) DO UPDATE
			SET redeemed = TRUE, redeemed_at = CURRENT_TIMESTAMP
		RETURNING team_name, redeemed, redeemed_at`,
		teamName).Scan(&r.TeamName, &r.Redeemed, &r.RedeemedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create redemption record: %w", err)
	}

	return &RedeemResult{
		Success:    true,
		TeamName:   teamName,
		Message:    fmt.Sprintf("Successfully redeemed present for team '%s'!", teamName),
		Redemption: &r,
	}, nil
}

// CheckEligibility checks if a staff pass ID is eligible for redemption
func (app *App) CheckEligibility(staffPassID string) (bool, string, error) {
	// Check if staff pass ID is valid and get team name
	var teamName string
	err := app.DB.QueryRow(`
		SELECT team_name 
		FROM staff_mappings 
		WHERE staff_pass_id = $1 
		ORDER BY created_at DESC 
		LIMIT 1`, staffPassID).Scan(&teamName)

	if err == sql.ErrNoRows {
		return false, "Invalid staff pass ID", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check eligibility: %w", err)
	}

	// Check if team has already redeemed
	var redeemed bool
	err = app.DB.QueryRow("SELECT redeemed FROM redemptions WHERE team_name = $1", teamName).Scan(&redeemed)
	if err != nil && err != sql.ErrNoRows {
		return false, "", fmt.Errorf("failed to check team redemption status: %w", err)
	}

	if redeemed {
		return false, fmt.Sprintf("Team '%s' has already redeemed", teamName), nil
	}

	return true, fmt.Sprintf("Team '%s' is eligible for redemption", teamName), nil
}
