package cache

import "context"

// CacheStore defines a pluggable cache interface for staff lookups
// and redemption status checks. Implementations must be safe for
// concurrent use by multiple goroutines.
type CacheStore interface {
	// GetStaffTeam returns the team name for a given staff pass ID.
	// found=false means the key is not cached (cache miss).
	GetStaffTeam(ctx context.Context, staffPassID string) (teamName string, found bool, err error)

	// SetStaffTeam caches the staff-to-team mapping.
	SetStaffTeam(ctx context.Context, staffPassID string, teamName string) error

	// GetRedemptionStatus returns whether a team has redeemed.
	// found=false means the key is not cached (cache miss).
	// This distinguishes "not cached" from "cached as false".
	GetRedemptionStatus(ctx context.Context, teamName string) (redeemed bool, found bool, err error)

	// SetRedemptionNX atomically sets the redemption status for a team
	// ONLY if it does not already exist (SETNX). Returns set=true if
	// this caller won the lock, set=false if another desk already set it.
	// This is the concurrency gate for multi-desk redemption.
	SetRedemptionNX(ctx context.Context, teamName string) (set bool, err error)

	// InvalidateRedemption removes the redemption status from cache.
	// Used when a redemption is reversed via the API or ops intervention.
	InvalidateRedemption(ctx context.Context, teamName string) error

	// Ping verifies the cache connection is alive.
	Ping(ctx context.Context) error

	// Close releases any resources held by the cache.
	Close() error
}
