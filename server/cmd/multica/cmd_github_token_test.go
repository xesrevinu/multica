package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchTaskGitHubToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer task-tok" {
			http.Error(w, "unknown task credential", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"token":"ghs_x","expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	if got, err := fetchTaskGitHubToken(context.Background(), srv.URL, "task-tok"); err != nil || got != "ghs_x" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := fetchTaskGitHubToken(context.Background(), srv.URL, "bad"); err == nil {
		t.Fatal("bad credential accepted")
	}
}
