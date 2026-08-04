package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/artifact"
)

const maxArtifactResponseBytes = 16 << 20

func registerArtifactRoutes(api *gin.RouterGroup, store artifact.Store) {
	api.GET("/artifacts/*artifactKey", func(c *gin.Context) {
		if store == nil {
			writeError(c, http.StatusServiceUnavailable, "artifact storage is unavailable")
			return
		}
		key := strings.TrimPrefix(c.Param("artifactKey"), "/")
		if key == "" {
			writeError(c, http.StatusBadRequest, "artifact key is required")
			return
		}
		reader, err := store.Open(c.Request.Context(), key)
		if err != nil {
			if errors.Is(err, artifact.ErrInvalidKey) {
				writeError(c, http.StatusBadRequest, "artifact key is invalid")
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				writeError(c, http.StatusNotFound, "artifact not found")
				return
			}
			writeError(c, http.StatusInternalServerError, "artifact could not be opened")
			return
		}
		defer reader.Close()
		payload, err := io.ReadAll(io.LimitReader(reader, maxArtifactResponseBytes+1))
		if err != nil {
			writeError(c, http.StatusInternalServerError, "artifact could not be read")
			return
		}
		if len(payload) > maxArtifactResponseBytes {
			writeError(c, http.StatusRequestEntityTooLarge, "artifact is too large to display")
			return
		}
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Content-Disposition", "inline")
		c.Data(http.StatusOK, "text/plain; charset=utf-8", payload)
	})
}
