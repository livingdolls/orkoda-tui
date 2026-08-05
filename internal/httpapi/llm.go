package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/credentials"
	"github.com/livingdolls/orkoda-tui/internal/llm"
	"github.com/livingdolls/orkoda-tui/internal/llmprovider"
)

type LLMProviderCatalog interface {
	List() []llm.ProviderInfo
}

type LLMProviderAdmin interface {
	Save(context.Context, string, llmprovider.SaveInput) (llm.ProviderInfo, error)
	Delete(context.Context, string) error
	Test(context.Context, string) (llmprovider.TestResult, error)
}

type LLMPolicyReader interface {
	Info() llm.PolicyInfo
}

func registerLLMRoutes(
	api *gin.RouterGroup,
	catalog LLMProviderCatalog,
	admin LLMProviderAdmin,
	policyReader LLMPolicyReader,
) {
	api.GET("/llm/providers", func(c *gin.Context) {
		if catalog == nil {
			writeData(c, http.StatusOK, []llm.ProviderInfo{})
			return
		}
		writeData(c, http.StatusOK, catalog.List())
	})

	api.PUT("/llm/providers/:provider", func(c *gin.Context) {
		if admin == nil {
			writeError(c, http.StatusServiceUnavailable, "LLM provider settings are unavailable")
			return
		}
		var input llmprovider.SaveInput
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, http.StatusBadRequest, "invalid LLM provider configuration")
			return
		}
		item, err := admin.Save(c.Request.Context(), strings.TrimSpace(c.Param("provider")), input)
		if err != nil {
			writeLLMProviderError(c, err)
			return
		}
		writeData(c, http.StatusOK, item)
	})

	api.DELETE("/llm/providers/:provider", func(c *gin.Context) {
		if admin == nil {
			writeError(c, http.StatusServiceUnavailable, "LLM provider settings are unavailable")
			return
		}
		if err := admin.Delete(c.Request.Context(), strings.TrimSpace(c.Param("provider"))); err != nil {
			writeLLMProviderError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})

	api.POST("/llm/providers/:provider/test", func(c *gin.Context) {
		if admin == nil {
			writeError(c, http.StatusServiceUnavailable, "LLM provider settings are unavailable")
			return
		}
		result, err := admin.Test(c.Request.Context(), strings.TrimSpace(c.Param("provider")))
		if err != nil {
			writeLLMProviderError(c, err)
			return
		}
		writeData(c, http.StatusOK, result)
	})

	api.GET("/llm/policy", func(c *gin.Context) {
		if policyReader == nil {
			writeData(c, http.StatusOK, llm.PolicyInfo{})
			return
		}
		writeData(c, http.StatusOK, policyReader.Info())
	})
}

func writeLLMProviderError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, llmprovider.ErrNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, llmprovider.ErrReadOnly):
		writeError(c, http.StatusConflict, err.Error())
	case errors.Is(err, llmprovider.ErrInvalid):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, credentials.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "secure credential storage is unavailable")
	default:
		writeError(c, http.StatusUnprocessableEntity, err.Error())
	}
}
