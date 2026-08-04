package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/livingdolls/orkoda-tui/internal/database"
)

func TestIdempotencyMiddlewareReplaysCompletedResponseAndRejectsHashConflict(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir()+"/orkoda.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLIdempotencyStore(db)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(idempotencyMiddleware(store))
	calls := 0
	router.POST("/command", func(c *gin.Context) {
		calls++
		c.JSON(http.StatusCreated, gin.H{"calls": calls})
	})
	request := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/command", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "same-key")
		router.ServeHTTP(recorder, req)
		return recorder
	}
	first := request(`{"value":1}`)
	second := request(`{"value":1}`)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated || first.Body.String() != second.Body.String() {
		t.Fatalf("responses = %d/%s and %d/%s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
	conflict := request(`{"value":2}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("hash conflict status = %d, body=%s", conflict.Code, conflict.Body.String())
	}
}
