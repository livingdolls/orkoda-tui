package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMetricsMiddlewareAndCounters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metrics := New()
	router := gin.New()
	router.Use(metrics.Middleware())
	router.GET("/ok", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/fail", func(c *gin.Context) { c.Status(http.StatusBadRequest) })
	for _, path := range []string{"/ok", "/fail"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	}
	metrics.StreamOpened()
	metrics.Reconnect()
	metrics.QueueRetry()
	metrics.QueueDead()
	metrics.PolicyDenied()
	metrics.StreamClosed()
	snapshot := metrics.Snapshot()
	if snapshot.Requests != 2 || snapshot.RequestErrors != 1 {
		t.Fatalf("request metrics = %#v", snapshot)
	}
	if snapshot.ActiveEventStreams != 0 || snapshot.Reconnects != 1 || snapshot.QueueRetries != 1 || snapshot.QueueDead != 1 || snapshot.PolicyDenials != 1 {
		t.Fatalf("counter metrics = %#v", snapshot)
	}
}
