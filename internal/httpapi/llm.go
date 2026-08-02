package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/llm"
)

type LLMProviderCatalog interface {
	List() []llm.ProviderInfo
}

type LLMPolicyReader interface {
	Info() llm.PolicyInfo
}

func registerLLMRoutes(api *gin.RouterGroup, catalog LLMProviderCatalog, policyReaders ...LLMPolicyReader) {
	api.GET("/llm/providers", func(c *gin.Context) {
		if catalog == nil {
			writeData(c, http.StatusOK, []llm.ProviderInfo{})
			return
		}
		writeData(c, http.StatusOK, catalog.List())
	})

	var policyReader LLMPolicyReader
	if len(policyReaders) > 0 {
		policyReader = policyReaders[0]
	}
	api.GET("/llm/policy", func(c *gin.Context) {
		if policyReader == nil {
			writeData(c, http.StatusOK, llm.PolicyInfo{})
			return
		}
		writeData(c, http.StatusOK, policyReader.Info())
	})
}
