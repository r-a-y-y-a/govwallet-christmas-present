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
// It uses SETNX via cache as an atomic concurrency gate so that only
// one desk can win the redemption for a given team.
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

	// ── Step 2: SETNX concurrency gate ─────────────────────────────────
	// If cache is available, use atomic SetRedemptionNX to serialize
	// concurrent desks. Only the first caller gets set=true.
	if app.Cache != nil {
		set, cacheErr := app.Cache.SetRedemptionNX(ctx, teamName)
		if cacheErr != nil {
			log.Printf("cache: SetRedemptionNX error (falling back to DB): %v", cacheErr)
			// Fall through to DB-only path below
		} else if !set {
			// Another desk already locked this team — fast reject.
			return &RedeemResult{
				Success:  false,
				TeamName: teamName,
				Message:  fmt.Sprintf("Team '%s' has already redeemed their present", teamName),
			}, nil
		}
		// set == true → this desk won the lock, proceed.
	} else {
		// No cache: fall back to DB check
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
	}

	// ── Step 3: write to PostgreSQL (source of truth) ──────────────────
	var r Redemption
	err = app.DB.QueryRow(`
		INSERT INTO redemptions (team_name, redeemed, redeemed_at)
		VALUES ($1, TRUE, CURRENT_TIMESTAMP)
		ON CONFLICT (team_name) DO UPDATE
			SET redeemed = TRUE, redeemed_at = CURRENT_TIMESTAMP
		RETURNING team_name, redeemed, redeemed_at`,
		teamName).Scan(&r.TeamName, &r.Redeemed, &r.RedeemedAt)

	if err != nil {
		// ── Step 4: rollback cache on DB failure ────────────────────
		if app.Cache != nil {
			if invErr := app.Cache.InvalidateRedemption(ctx, teamName); invErr != nil {
				log.Printf("cache: InvalidateRedemption rollback error: %v", invErr)
			}
		}
		return nil, fmt.Errorf("failed to create redemption record: %w", err)
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
func (app *App) CheckEligibility(staffPassID string) (bool, string, error) {
	ctx := context.Background()

	// ── Step 1: resolve staff → team (cache-aside) ─────────────────────
	teamName, err := app.resolveTeamName(ctx, staffPassID)
	if err == sql.ErrNoRows {
		return false, "Invalid staff pass ID", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check eligibility: %w", err)
	}

	// ── Step 2: check redemption status (cache-aside) ──────────────────
	if app.Cache != nil {
		redeemed, found, cacheErr := app.Cache.GetRedemptionStatus(ctx, teamName)
		if cacheErr != nil {
			log.Printf("cache: GetRedemptionStatus error (falling back to DB): %v", cacheErr)
		} else if found {
			if redeemed {
				return false, fmt.Sprintf("Team '%s' has already redeemed", teamName), nil
			}
			return true, fmt.Sprintf("Team '%s' is eligible for redemption", teamName), nil
		}
	}

	// Cache miss or no cache — fall back to DB
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
