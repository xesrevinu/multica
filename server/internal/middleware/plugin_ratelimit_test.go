package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPluginRateLimitWithoutLimiterFailsOpen(t *testing.T) {
	handler := PluginRateLimit(1, time.Minute)(okHandler)
	for i := 0; i < 3; i++ {
		request := httptest.NewRequest(http.MethodGet, "/v1/context", nil)
		request.Header.Set("Authorization", "Bearer mpi_local")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("call %d status = %d, want fail-open", i, response.Code)
		}
	}
}
