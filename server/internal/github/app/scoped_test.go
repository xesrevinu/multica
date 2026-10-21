package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A scoped mint asks GitHub for exactly one repo and the given permissions,
// and is not served from (or written to) the shared token cache.
func TestMintScopedNarrowsRepoAndPermissions(t *testing.T) {
	var got struct {
		Repositories []string          `json:"repositories"`
		Permissions  map[string]string `json:"permissions"`
	}
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/7/access_tokens" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_scoped", "expires_at": exp.Format(time.RFC3339)})
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, time.Now)
	tok, err := c.MintScoped(context.Background(), 7, "holt", map[string]string{"contents": "write", "pull_requests": "write"})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token != "ghs_scoped" || !tok.ExpiresAt.Equal(exp) {
		t.Fatalf("token %+v", tok)
	}
	if len(got.Repositories) != 1 || got.Repositories[0] != "holt" || got.Permissions["contents"] != "write" || len(got.Permissions) != 2 {
		t.Fatalf("request %+v", got)
	}
	if _, ok := c.cached(7); ok {
		t.Fatal("scoped token must not enter the shared cache")
	}
}

func TestMintScopedHidesErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"secret-ish detail"}`))
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, time.Now)
	_, err := c.MintScoped(context.Background(), 7, "holt", map[string]string{"contents": "write"})
	if err == nil || err.Error() != "github scoped installation token: unexpected status 422" {
		t.Fatalf("err = %v", err)
	}
}
