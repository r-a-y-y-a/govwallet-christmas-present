package api

import (
	"database/sql"
	"time"

	"github.com/hanju/govtech-christmas/api/cache"
)

// App holds shared dependencies for all handlers and services.
type App struct {
	DB    *sql.DB
	Cache cache.CacheStore
}

type StaffMapping struct {
	ID          int    `json:"id" db:"id"`
	StaffPassID string `json:"staff_pass_id" db:"staff_pass_id"`
	TeamName    string `json:"team_name" db:"team_name"`
	CreatedAt   int64  `json:"created_at" db:"created_at"`
}

type Redemption struct {
	TeamName   string    `json:"team_name" db:"team_name"`
	Redeemed   bool      `json:"redeemed" db:"redeemed"`
	RedeemedAt time.Time `json:"redeemed_at" db:"redeemed_at"`
}
