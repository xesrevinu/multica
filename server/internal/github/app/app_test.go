package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, base string, now func() time.Time) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{AppID: "123", PrivateKey: key, APIBase: base, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func tokenHandler(mints *int32, exp time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(mints, 1)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"token":"ghs_%d","expires_at":%q,"permissions":{"contents":"write"}}`, atomic.LoadInt32(mints), exp.UTC().Format(time.RFC3339))
	}
}

func TestDisabledClient(t *testing.T) {
	var c *Client
	if c.Enabled() {
		t.Fatal("nil client enabled")
	}
	if err := c.Do(context.Background(), 1, http.MethodGet, "/x", nil, nil); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	if c, err := FromEnv(); c != nil || err != nil {
		t.Fatalf("FromEnv unset = %v, %v", c, err)
	}
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "not a key")
	if _, err := FromEnv(); err == nil || strings.Contains(err.Error(), "not a key") {
		t.Fatalf("bad key err = %v", err)
	}
}

// Tokens are cached per installation, minted once under concurrency and
// renewed once the clock passes expiry - skew.
func TestTokenCacheRenewAndSingleflight(t *testing.T) {
	var mints int32
	now := time.Now()
	clock := func() time.Time { return now }
	srv := httptest.NewServer(tokenHandler(&mints, now.Add(time.Hour)))
	defer srv.Close()
	c := testClient(t, srv.URL, func() time.Time { return clock() })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.Token(context.Background(), 7) }()
	}
	wg.Wait()
	if mints != 1 {
		t.Fatalf("mints = %d, want 1", mints)
	}
	if p, _ := c.Permissions(context.Background(), 7); p["contents"] != "write" {
		t.Fatalf("permissions %v", p)
	}
	if _, err := c.Token(context.Background(), 8); err != nil || mints != 2 {
		t.Fatalf("second installation: mints=%d err=%v", mints, err)
	}
	later := now.Add(56 * time.Minute) // inside the 5-minute renew skew
	clock = func() time.Time { return later }
	if _, err := c.Token(context.Background(), 7); err != nil || mints != 3 {
		t.Fatalf("renew: mints=%d err=%v", mints, err)
	}
}

// GETs send If-None-Match; a 304 replays the cached body. Rate headers are
// tracked, and a spent budget short-circuits without calling GitHub.
func TestETagAndRateLimit(t *testing.T) {
	var mints, gets, conditional int32
	reset := time.Now().Add(10 * time.Minute).Unix()
	remaining := "4999"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			tokenHandler(&mints, time.Now().Add(time.Hour))(w, r)
			return
		}
		atomic.AddInt32(&gets, 1)
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", remaining)
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset))
		if r.Header.Get("If-None-Match") == `"v1"` {
			atomic.AddInt32(&conditional, 1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`{"n":1}`))
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, nil)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		var out struct{ N int }
		if err := c.Do(ctx, 1, http.MethodGet, "/repos/o/r/pulls/1", nil, &out); err != nil || out.N != 1 {
			t.Fatalf("get %d: %v %+v", i, err, out)
		}
	}
	if conditional != 1 {
		t.Fatalf("conditional = %d, want 1", conditional)
	}
	if rs := c.Rate(1); rs.Remaining != 4999 || rs.Limit != 5000 || rs.Low(time.Now()) {
		t.Fatalf("rate %+v", rs)
	}
	remaining = "0"
	_ = c.Do(ctx, 1, http.MethodGet, "/repos/o/r/pulls/2", nil, nil)
	before := gets
	var rl *RateLimitError
	if err := c.Do(ctx, 1, http.MethodGet, "/repos/o/r/pulls/3", nil, nil); !errors.As(err, &rl) {
		t.Fatalf("spent budget err = %v", err)
	}
	if gets != before {
		t.Fatal("spent budget still called GitHub")
	}
}

func TestRateLimitedResponses(t *testing.T) {
	var mints int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			tokenHandler(&mints, time.Now().Add(time.Hour))(w, r)
		case r.URL.Path == "/secondary":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
		case r.URL.Path == "/retry":
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
		}
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, nil)
	ctx := context.Background()
	var rl *RateLimitError
	if err := c.Do(ctx, 1, http.MethodGet, "/secondary", nil, nil); !errors.As(err, &rl) {
		t.Fatalf("secondary: %v", err)
	}
	if err := c.Do(ctx, 1, http.MethodGet, "/retry", nil, nil); !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second {
		t.Fatalf("retry-after: %v", err)
	}
	err := c.Do(ctx, 1, http.MethodPost, "/perm", map[string]any{}, nil)
	if !IsStatus(err, http.StatusForbidden) || strings.Contains(err.Error(), "ghs_") {
		t.Fatalf("permission 403: %v", err)
	}
}

// A 401 drops the cached token and retries once with a fresh mint.
func TestUnauthorizedRemints(t *testing.T) {
	var mints, calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			tokenHandler(&mints, time.Now().Add(time.Hour))(w, r)
			return
		}
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, nil)
	if err := c.Do(context.Background(), 1, http.MethodGet, "/x", nil, nil); err != nil || mints != 2 {
		t.Fatalf("err=%v mints=%d", err, mints)
	}
}

func TestInstallationForAndBot(t *testing.T) {
	var mints, instCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/55/access_tokens":
			tokenHandler(&mints, time.Now().Add(time.Hour))(w, r)
		case "/repos/Owner/Repo/installation":
			atomic.AddInt32(&instCalls, 1)
			_, _ = w.Write([]byte(`{"id":55}`))
		case "/app":
			_, _ = w.Write([]byte(`{"slug":"multica-bot"}`))
		case "/users/multica-bot[bot]":
			_, _ = w.Write([]byte(`{"id":4242,"login":"multica-bot[bot]"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := testClient(t, srv.URL, nil)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if id, err := c.InstallationFor(ctx, "Owner", "Repo"); err != nil || id != 55 {
			t.Fatalf("installation = %d, %v", id, err)
		}
	}
	if instCalls != 1 {
		t.Fatalf("installation lookups = %d, want 1 (cached)", instCalls)
	}
	b, err := c.Bot(ctx, 55)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Co-authored-by: multica-bot[bot] <4242+multica-bot[bot]@users.noreply.github.com>"; b.CoAuthoredBy() != want {
		t.Fatalf("trailer %q", b.CoAuthoredBy())
	}
}
