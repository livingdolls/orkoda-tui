package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const protocolVersion = "v1"

type healthResponse struct {
	Status          string `json:"status"`
	ProtocolVersion string `json:"protocol_version"`
	Timestamp       string `json:"timestamp"`
}

func NewRouter(environment string) *gin.Engine {
	if environment != "development" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, healthResponse{
			Status:          "ok",
			ProtocolVersion: protocolVersion,
			Timestamp:       time.Now().UTC().Format(time.RFC3339),
		})
	})

	api := router.Group("/api/v1")
	api.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"service": "orkoda-local-daemon",
				"status":  "ready",
			},
			"meta": gin.H{
				"protocol_version": protocolVersion,
			},
		})
	})

	return router
}
