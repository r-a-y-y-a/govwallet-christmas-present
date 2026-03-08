package api

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(app *App) *gin.Engine {
	router := gin.Default()

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		dbOK := app.DB.Ping() == nil

		cacheOK := true
		if app.Cache != nil {
			cacheOK = app.Cache.Ping(c.Request.Context()) == nil
		}

		status := "healthy"
		httpCode := http.StatusOK
		if !dbOK {
			status = "unhealthy"
			httpCode = http.StatusServiceUnavailable
		} else if !cacheOK {
			status = "degraded"
		}

		c.JSON(httpCode, gin.H{
			"status":   status,
			"service":  "govtech-christmas-redemption",
			"database": dbOK,
			"cache":    cacheOK,
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

	// Sync cache so the SETNX gate is aware of this redemption
	if app.Cache != nil && r.Redeemed {
		if cacheErr := app.Cache.SetRedemptionStatus(c.Request.Context(), r.TeamName); cacheErr != nil {
			log.Printf("cache: SetRedemptionStatus error for %s: %v", r.TeamName, cacheErr)
		}
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

	// Sync cache: mark redeemed or clear so team can redeem again
	if app.Cache != nil {
		if r.Redeemed {
			if cacheErr := app.Cache.SetRedemptionStatus(c.Request.Context(), teamName); cacheErr != nil {
				log.Printf("cache: SetRedemptionStatus error for %s: %v", teamName, cacheErr)
			}
		} else {
			if cacheErr := app.Cache.InvalidateRedemption(c.Request.Context(), teamName); cacheErr != nil {
				log.Printf("cache: InvalidateRedemption error for %s: %v", teamName, cacheErr)
			}
		}
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

	// Invalidate cache so the team can redeem again
	if app.Cache != nil {
		if invErr := app.Cache.InvalidateRedemption(c.Request.Context(), teamName); invErr != nil {
			log.Printf("cache: InvalidateRedemption error for %s: %v", teamName, invErr)
		}
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

	m, err := app.findStaffMappingByPassID(staffPassID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Staff mapping not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch staff mapping"})
		return
	}

	// Populate cache for future resolveTeamName lookups
	if app.Cache != nil {
		_ = app.Cache.SetStaffTeam(c.Request.Context(), staffPassID, m.TeamName)
	}

	c.JSON(http.StatusOK, m)
}

// Lookup staff pass ID to get team information
func (app *App) lookupStaffPass(c *gin.Context) {
	staffPassID := c.Param("staff_pass_id")

	m, err := app.findStaffMappingByPassID(staffPassID)
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

	// Populate cache for future resolveTeamName lookups
	if app.Cache != nil {
		_ = app.Cache.SetStaffTeam(c.Request.Context(), staffPassID, m.TeamName)
	}

	c.JSON(http.StatusOK, gin.H{
		"staff_pass_id":      m.StaffPassID,
		"team_name":          m.TeamName,
		"valid":              true,
		"mapping_created_at": m.CreatedAt,
	})
}

// Check eligibility for redemption — delegates to service layer (cache-aware)
func (app *App) checkEligibility(c *gin.Context) {
	staffPassID := c.Param("staff_pass_id")

	eligible, teamName, reason, err := app.CheckEligibility(staffPassID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := gin.H{
		"staff_pass_id": staffPassID,
		"team_name":     teamName,
		"eligible":      eligible,
		"reason":        reason,
	}
	c.JSON(http.StatusOK, resp)
}

// Redeem for a specific staff member — delegates to service layer (cache-aware + SETNX)
func (app *App) redeemForStaff(c *gin.Context) {
	staffPassID := c.Param("staff_pass_id")

	result, err := app.RedeemPresent(staffPassID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !result.Success {
		// Distinguish invalid staff from already-redeemed
		if result.TeamName == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":         result.Message,
				"staff_pass_id": staffPassID,
			})
		} else {
			c.JSON(http.StatusConflict, gin.H{
				"error":     result.Message,
				"team_name": result.TeamName,
			})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":    "Redemption successful",
		"redemption": result.Redemption,
	})
}
