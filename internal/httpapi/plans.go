package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/plans"
)

type PlanRegistry interface {
	Create(context.Context, string, string, plans.VersionInput) (plans.Plan, error)
	AddVersion(context.Context, string, plans.VersionInput) (plans.Plan, error)
	ListProject(context.Context, string) ([]plans.Plan, error)
	Get(context.Context, string) (plans.Plan, error)
	Update(context.Context, string, string, plans.Status) (plans.Plan, error)
	Delete(context.Context, string) error
}

type createPlanRequest struct {
	Title              string   `json:"title"`
	Requirement        string   `json:"requirement"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Constraints        []string `json:"constraints"`
}

type updatePlanRequest struct {
	Title  string       `json:"title"`
	Status plans.Status `json:"status"`
}

type createPlanVersionRequest struct {
	Requirement        string   `json:"requirement"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Constraints        []string `json:"constraints"`
}

func registerPlanRoutes(api *gin.RouterGroup, registry PlanRegistry) {
	api.POST("/projects/:projectID/plans", func(c *gin.Context) {
		if !requirePlanRegistry(c, registry) {
			return
		}
		var request createPlanRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain title and requirement")
			return
		}
		plan, err := registry.Create(c.Request.Context(), c.Param("projectID"), request.Title, plans.VersionInput{
			Requirement:        request.Requirement,
			AcceptanceCriteria: request.AcceptanceCriteria,
			Constraints:        request.Constraints,
		})
		if err != nil {
			writePlanError(c, err)
			return
		}
		writeData(c, http.StatusCreated, plan)
	})

	api.GET("/projects/:projectID/plans", func(c *gin.Context) {
		if !requirePlanRegistry(c, registry) {
			return
		}
		planList, err := registry.ListProject(c.Request.Context(), c.Param("projectID"))
		if err != nil {
			writePlanError(c, err)
			return
		}
		writeData(c, http.StatusOK, planList)
	})

	api.GET("/plans/:planID", func(c *gin.Context) {
		if !requirePlanRegistry(c, registry) {
			return
		}
		plan, err := registry.Get(c.Request.Context(), c.Param("planID"))
		if err != nil {
			writePlanError(c, err)
			return
		}
		writeData(c, http.StatusOK, plan)
	})

	api.PATCH("/plans/:planID", func(c *gin.Context) {
		if !requirePlanRegistry(c, registry) {
			return
		}
		var request updatePlanRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain title and status")
			return
		}
		plan, err := registry.Update(c.Request.Context(), c.Param("planID"), request.Title, request.Status)
		if err != nil {
			writePlanError(c, err)
			return
		}
		writeData(c, http.StatusOK, plan)
	})

	api.POST("/plans/:planID/versions", func(c *gin.Context) {
		if !requirePlanRegistry(c, registry) {
			return
		}
		var request createPlanVersionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, http.StatusBadRequest, "request body must contain requirement")
			return
		}
		plan, err := registry.AddVersion(c.Request.Context(), c.Param("planID"), plans.VersionInput{
			Requirement:        request.Requirement,
			AcceptanceCriteria: request.AcceptanceCriteria,
			Constraints:        request.Constraints,
		})
		if err != nil {
			writePlanError(c, err)
			return
		}
		writeData(c, http.StatusCreated, plan)
	})

	api.DELETE("/plans/:planID", func(c *gin.Context) {
		if !requirePlanRegistry(c, registry) {
			return
		}
		if err := registry.Delete(c.Request.Context(), c.Param("planID")); err != nil {
			writePlanError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}

func requirePlanRegistry(c *gin.Context, registry PlanRegistry) bool {
	if registry == nil {
		writeError(c, http.StatusServiceUnavailable, "plan registry is unavailable")
		return false
	}
	return true
}

func writePlanError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, plans.ErrInvalidPlan):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, plans.ErrProjectNotFound), errors.Is(err, plans.ErrNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "plan operation failed")
	}
}
