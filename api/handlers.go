package api

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type App struct {
	DB *sql.DB
}

func SetupRoutes(app *App) *gin.Engine {
	router := gin.Default()

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "govtech-christmas-redemption",
		})
	})

	// API routes
	apiGroup := router.Group("/api/v1")
	{
		// Redemption endpoints
		apiGroup.GET("/redemptions", app.getRedemptions)
		apiGroup.GET("/redemptions/:team_name", app.getRedemption)
		apiGroup.POST("/redemptions", app.createRedemption)
		apiGroup.PUT("/redemptions/:team_name", app.updateRedemption)
		apiGroup.DELETE("/redemptions/:team_name", app.deleteRedemption)

		// Staff mapping endpoints
		apiGroup.GET("/staff-mappings", app.getStaffMappings)
		apiGroup.GET("/staff-mappings/:staff_pass_id", app.getStaffMapping)

		// Eligibility and lookup endpoints
		apiGroup.GET("/lookup/:staff_pass_id", app.lookupStaffPass)
		apiGroup.GET("/eligibility/:staff_pass_id", app.checkEligibility)
		apiGroup.POST("/redeem/:staff_pass_id", app.redeemForStaff)
	}

	return router
}

func (app *App) getRedemptions(c *gin.Context) {
	rows, err := app.DB.Query("SELECT team_name, redeemed, redeemed_at FROM redemptions")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch redemptions"})
		return
	}
	defer rows.Close()

	var redemptions []Redemption
	for rows.Next() {
		var r Redemption
		err := rows.Scan(&r.TeamName, &r.Redeemed, &r.RedeemedAt)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		redemptions = append(redemptions, r)
	}

	c.JSON(http.StatusOK, redemptions)
}

func (app *App) getRedemption(c *gin.Context) {
	teamName := c.Param("team_name")

	var r Redemption
	err := app.DB.QueryRow("SELECT team_name, redeemed, redeemed_at FROM redemptions WHERE team_name = $1", teamName).
		Scan(&r.TeamName, &r.Redeemed, &r.RedeemedAt)

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
		INSERT INTO redemptions (team_name, redeemed)
		VALUES ($1, $2)
		RETURNING redeemed_at`,
		r.TeamName, r.Redeemed).Scan(&r.RedeemedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create redemption"})
		return
	}

	c.JSON(http.StatusCreated, r)
}

func (app *App) updateRedemption(c *gin.Context) {
	teamName := c.Param("team_name")
	var r Redemption

	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := app.DB.Exec(`
		UPDATE redemptions
		SET redeemed = $1,
		    redeemed_at = CASE WHEN $1 THEN CURRENT_TIMESTAMP ELSE redeemed_at END
		WHERE team_name = $2`,
		r.Redeemed, teamName)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update redemption"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Redemption not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Redemption updated successfully"})
}

func (app *App) deleteRedemption(c *gin.Context) {
	teamName := c.Param("team_name")

	result, err := app.DB.Exec("DELETE FROM redemptions WHERE team_name = $1", teamName)
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
			"error":         "Invalid staff pass ID",
			"staff_pass_id": staffPassID,
			"valid":         false,
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to lookup staff pass"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"staff_pass_id":      m.StaffPassID,
		"team_name":          m.TeamName,
		"valid":              true,
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
			"eligible":      false,
			"reason":        "Invalid staff pass ID",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check eligibility"})
		return
	}

	// Check if team has already redeemed
	var redeemed bool
	err = app.DB.QueryRow("SELECT redeemed FROM redemptions WHERE team_name = $1", teamName).Scan(&redeemed)
	if err != nil && err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check team redemption status"})
		return
	}

	if redeemed {
		c.JSON(http.StatusOK, gin.H{
			"staff_pass_id": staffPassID,
			"team_name":     teamName,
			"eligible":      false,
			"reason":        "Team has already redeemed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"staff_pass_id": staffPassID,
		"team_name":     teamName,
		"eligible":      true,
		"reason":        "Team is eligible for redemption",
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
			"error":         "Invalid staff pass ID",
			"staff_pass_id": staffPassID,
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate staff pass"})
		return
	}

	// Check if team has already redeemed
	var redeemed bool
	err = app.DB.QueryRow("SELECT redeemed FROM redemptions WHERE team_name = $1", teamName).Scan(&redeemed)
	if err != nil && err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check team redemption status"})
		return
	}

	if redeemed {
		c.JSON(http.StatusConflict, gin.H{
			"error":     "Team has already redeemed",
			"team_name": teamName,
		})
		return
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create redemption record"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":    "Redemption successful",
		"redemption": r,
	})
}
