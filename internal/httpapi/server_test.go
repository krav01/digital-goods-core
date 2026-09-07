package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHandlerStatusEndpoints(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()

			NewHandler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q, want JSON", contentType)
			}
			if body := response.Body.String(); body != "{\"status\":\"ok\"}\n" {
				t.Errorf("body = %q, want status JSON", body)
			}
		})
	}
}
