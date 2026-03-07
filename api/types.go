package api

import "time"

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
