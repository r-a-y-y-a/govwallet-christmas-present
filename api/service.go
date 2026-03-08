package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// RedeemResult contains the result of a redemption attempt
type RedeemResult struct {
	Success    bool
	TeamName   string
	Message    string
	Redemption *Redemption
}

// findStaffMappingByPassID returns the latest staff mapping for a given
// staff pass ID, or sql.ErrNoRows if none exists.
func (app *App) findStaffMappingByPassID(staffPassID string) (*StaffMapping, error) {
	var m StaffMapping
	err := app.DB.QueryRow(`
		SELECT id, staff_pass_id, team_name, created_at
		FROM staff_mappings
		WHERE staff_pass_id = $1
		ORDER BY created_at DESC
		LIMIT 1`, staffPassID).Scan(&m.ID, &m.StaffPassID, &m.TeamName, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// resolveTeamName looks up the team name for a staff pass ID,
// checking the cache first (cache-aside) then falling back to the DB.
func (app *App) resolveTeamName(ctx context.Context, staffPassID string) (string, error) {
	// 1. Cache hit?
	if app.Cache != nil {
		team, found, err := app.Cache.GetStaffTeam(ctx, staffPassID)
		if err != nil {
			log.Printf("cache: GetStaffTeam error (falling back to DB): %v", err)
		} else if found {
			return team, nil
		}
	}

	// 2. DB lookup
	m, err := app.findStaffMappingByPassID(staffPassID)
	if err != nil {
		return "", err // caller handles sql.ErrNoRows
	}

	// 3. Populate cache on miss
	if app.Cache != nil {
		if cacheErr := app.Cache.SetStaffTeam(ctx, staffPassID, m.TeamName); cacheErr != nil {
			log.Printf("cache: SetStaffTeam error: %v", cacheErr)
		}
	}
	return m.TeamName, nil
}

// RedeemPresent attempts to redeem a present for a given staff pass ID.
// It writes to PostgreSQL first (source of truth + concurrency gate),
// then updates the Redis cache on success.
func (app *App) RedeemPresent(staffPassID string) (*RedeemResult, error) {
	ctx := context.Background()

	// ── Step 1: resolve staff → team (cache-aside) ─────────────────────
	teamName, err := app.resolveTeamName(ctx, staffPassID)
	if err == sql.ErrNoRows {
		return &RedeemResult{
			Success: false,
			Message: fmt.Sprintf("Invalid staff pass ID: %s", staffPassID),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to validate staff pass: %w", err)
	}

	// ── Step 2: cache fast-reject (performance optimisation) ───────────
	// If Redis already shows this team as redeemed, skip the DB round-trip.
	if app.Cache != nil {
		redeemed, found, cacheErr := app.Cache.GetRedemptionStatus(ctx, teamName)
		if cacheErr != nil {
			log.Printf("cache: GetRedemptionStatus error (falling back to DB): %v", cacheErr)
		} else if found && redeemed {
			return &RedeemResult{
				Success:  false,
				TeamName: teamName,
				Message:  fmt.Sprintf("Team '%s' has already redeemed their present", teamName),
			}, nil
		}
	}

	// ── Step 3: DB write (source of truth + concurrency gate) ──────────
	// The WHERE clause ensures the UPDATE only fires when the existing
	// row has redeemed=FALSE. If the team is already redeemed, no row
	// is returned and we get sql.ErrNoRows — this is the DB-level
	// concurrency gate that serialises concurrent desk requests.
	var r Redemption
	err = app.DB.QueryRow(`
		INSERT INTO redemptions (team_name, redeemed, redeemed_at)
		VALUES ($1, TRUE, CURRENT_TIMESTAMP)
		ON CONFLICT (team_name) DO UPDATE
			SET redeemed = TRUE, redeemed_at = CURRENT_TIMESTAMP
			WHERE redemptions.redeemed = FALSE
		RETURNING team_name, redeemed, redeemed_at`,
		teamName).Scan(&r.TeamName, &r.Redeemed, &r.RedeemedAt)

	if err == sql.ErrNoRows {
		// Team already redeemed — populate cache for future fast rejects
		if app.Cache != nil {
			if cacheErr := app.Cache.SetRedemptionStatus(ctx, teamName); cacheErr != nil {
				log.Printf("cache: SetRedemptionStatus error: %v", cacheErr)
			}
		}
		return &RedeemResult{
			Success:  false,
			TeamName: teamName,
			Message:  fmt.Sprintf("Team '%s' has already redeemed their present", teamName),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create redemption record: %w", err)
	}

	// ── Step 4: DB succeeded — update Redis cache ──────────────────────
	if app.Cache != nil {
		if cacheErr := app.Cache.SetRedemptionStatus(ctx, teamName); cacheErr != nil {
			log.Printf("cache: SetRedemptionStatus error: %v", cacheErr)
		}
	}

	return &RedeemResult{
		Success:    true,
		TeamName:   teamName,
		Message:    fmt.Sprintf("Successfully redeemed present for team '%s'!", teamName),
		Redemption: &r,
	}, nil
}

// CheckEligibility checks if a staff pass ID is eligible for redemption.
// Uses cache-aside for both staff mapping and redemption status lookups.
func (app *App) CheckEligibility(staffPassID string) (bool, string, string, error) {
	ctx := context.Background()

	// ── Step 1: resolve staff → team (cache-aside) ─────────────────────
	teamName, err := app.resolveTeamName(ctx, staffPassID)
	if err == sql.ErrNoRows {
		return false, "", "Invalid staff pass ID", nil
	}
	if err != nil {
		return false, "", "", fmt.Errorf("failed to check eligibility: %w", err)
	}

	// ── Step 2: check redemption status (cache-aside) ──────────────────
	if app.Cache != nil {
		redeemed, found, cacheErr := app.Cache.GetRedemptionStatus(ctx, teamName)
		if cacheErr != nil {
			log.Printf("cache: GetRedemptionStatus error (falling back to DB): %v", cacheErr)
		} else if found {
			if redeemed {
				return false, teamName, fmt.Sprintf("Team '%s' has already redeemed", teamName), nil
			}
			return true, teamName, fmt.Sprintf("Team '%s' is eligible for redemption", teamName), nil
		}
	}

	// Cache miss or no cache — fall back to DB
	var redeemed bool
	err = app.DB.QueryRow("SELECT redeemed FROM redemptions WHERE team_name = $1", teamName).Scan(&redeemed)
	if err != nil && err != sql.ErrNoRows {
		return false, "", "", fmt.Errorf("failed to check team redemption status: %w", err)
	}

	if redeemed {
		return false, teamName, fmt.Sprintf("Team '%s' has already redeemed", teamName), nil
	}

	return true, teamName, fmt.Sprintf("Team '%s' is eligible for redemption", teamName), nil
}
