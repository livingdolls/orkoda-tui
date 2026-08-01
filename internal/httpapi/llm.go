package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/llm"
)

type LLMProviderCatalog interface {
	List() []llm.ProviderInfo
}

func registerLLMRoutes(api *gin.RouterGroup, catalog LLMProviderCatalog) {
	api.GET("/llm/providers", func(c *gin.Context) {
		if catalog == nil {
			writeData(c, http.StatusOK, []llm.ProviderInfo{})
			return
		}
		writeData(c, http.StatusOK, catalog.List())
	})
}
