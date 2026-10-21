// Package app is the server's GitHub App client: App JWT → per-installation
// access token (cached, renewed before its one-hour expiry, concurrent mints
// collapsed) → REST / GraphQL calls with conditional requests and rate-limit
// tracking. It is the one place server code talks to GitHub as the App; the
// PR snapshot pipeline (ghsnapshot), the orchestrator engine and the PR
// actions all share a single process-wide Client (Shared) so they share one
// token cache, one ETag cache and one view of the remaining budget.
//
// Credential hygiene: the private key and installation tokens never appear in
// logs or error values. Errors carry a status code and GitHub's "message"
// field at most.
package app

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"
)

const (
	DefaultAPIBase = "https://api.github.com"
	// TokenRenewSkew renews an installation token this long before it expires
	// so an in-flight request never races the expiry boundary.
	TokenRenewSkew = 5 * time.Minute
	// installationTTL is how long a repo → installation lookup is cached.
	installationTTL = time.Hour
)

// Config builds a Client. Exactly one of PrivateKey / PrivateKeyPEM is needed.
type Config struct {
	AppID         string
	PrivateKeyPEM string
	PrivateKey    *rsa.PrivateKey
	// Slug is the App slug (GITHUB_APP_SLUG); optional, saves a GET /app.
	Slug       string
	APIBase    string
	HTTPClient *http.Client
	Now        func() time.Time
	// ETagEntries caps the conditional-request cache (default 1024).
	ETagEntries int
}

// Client is safe for concurrent use. A nil *Client is a valid "App not
// configured" value: Enabled reports false and calls return ErrDisabled.
type Client struct {
	appID   string
	key     *rsa.PrivateKey
	slug    string
	apiBase string
	http    *http.Client
	now     func() time.Time

	mu     sync.Mutex
	tokens map[int64]token
	insts  map[string]instEntry
	rate   map[int64]RateState
	etags  *etagCache
	bot    *Bot
	sf     singleflight.Group
}

type token struct {
	value       string
	expiry      time.Time
	permissions map[string]string
}

type instEntry struct {
	id      int64
	fetched time.Time
}

// ErrDisabled is returned by every call on a client without App credentials.
var ErrDisabled = errors.New("github app not configured")

// New builds a Client from cfg.
func New(cfg Config) (*Client, error) {
	key := cfg.PrivateKey
	if key == nil {
		k, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(strings.TrimSpace(cfg.PrivateKeyPEM)))
		if err != nil {
			// Never include key material in the error.
			return nil, fmt.Errorf("parse GitHub App private key: %w", err)
		}
		key = k
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, errors.New("github app: empty app id")
	}
	c := &Client{
		appID:   strings.TrimSpace(cfg.AppID),
		key:     key,
		slug:    strings.TrimSpace(cfg.Slug),
		apiBase: strings.TrimRight(cfg.APIBase, "/"),
		http:    cfg.HTTPClient,
		now:     cfg.Now,
		tokens:  map[int64]token{},
		insts:   map[string]instEntry{},
		rate:    map[int64]RateState{},
		etags:   newETagCache(cfg.ETagEntries),
	}
	if c.apiBase == "" {
		c.apiBase = DefaultAPIBase
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 20 * time.Second}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// FromEnv builds a Client from GITHUB_APP_ID, GITHUB_APP_PRIVATE_KEY and
// (optional) GITHUB_APP_SLUG / GITHUB_API_URL. Both id and key unset →
// (nil, nil): the App is not configured and callers degrade.
func FromEnv() (*Client, error) {
	id := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	pem := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if id == "" || pem == "" {
		return nil, nil
	}
	return New(Config{AppID: id, PrivateKeyPEM: pem, Slug: os.Getenv("GITHUB_APP_SLUG"), APIBase: os.Getenv("GITHUB_API_URL")})
}

var (
	sharedMu  sync.Mutex
	sharedFP  string
	shared    *Client
	sharedErr error
)

// Shared returns the process-wide Client built from the environment, so every
// server component uses one token cache and one rate-limit view. It is
// rebuilt only when the App environment changes (tests).
func Shared() (*Client, error) {
	fp := sha256.Sum256([]byte(os.Getenv("GITHUB_APP_ID") + "\x00" + os.Getenv("GITHUB_APP_PRIVATE_KEY") + "\x00" +
		os.Getenv("GITHUB_APP_SLUG") + "\x00" + os.Getenv("GITHUB_API_URL")))
	key := hex.EncodeToString(fp[:])
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if key != sharedFP {
		sharedFP = key
		shared, sharedErr = FromEnv()
	}
	return shared, sharedErr
}

// Enabled reports whether App credentials are configured.
func (c *Client) Enabled() bool { return c != nil && c.key != nil }

// AppID returns the configured App id.
func (c *Client) AppID() string {
	if c == nil {
		return ""
	}
	return c.appID
}

// JWT mints the short-lived RS256 App JWT (iat back-dated 60s for clock skew,
// exp 9 minutes; GitHub's ceiling is 10).
func (c *Client) JWT() (string, error) {
	if !c.Enabled() {
		return "", ErrDisabled
	}
	now := c.now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": c.appID,
	})
	s, err := tok.SignedString(c.key)
	if err != nil {
		return "", errors.New("sign App JWT failed")
	}
	return s, nil
}

// Token returns a cached installation access token, minting one when the
// cache is empty or within TokenRenewSkew of expiry. Concurrent callers for
// one installation share a single mint.
func (c *Client) Token(ctx context.Context, installationID int64) (string, error) {
	if !c.Enabled() {
		return "", ErrDisabled
	}
	if t, ok := c.cached(installationID); ok {
		return t, nil
	}
	v, err, _ := c.sf.Do("tok:"+strconv.FormatInt(installationID, 10), func() (any, error) {
		if t, ok := c.cached(installationID); ok {
			return t, nil
		}
		return c.mint(ctx, installationID)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (c *Client) cached(id int64) (string, bool) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[id]; ok && now.Add(TokenRenewSkew).Before(t.expiry) {
		return t.value, true
	}
	return "", false
}

// Invalidate drops the cached token of an installation (e.g. after a 401).
func (c *Client) Invalidate(installationID int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.tokens, installationID)
	c.mu.Unlock()
}

func (c *Client) mint(ctx context.Context, id int64) (string, error) {
	jwtStr, err := c.JWT()
	if err != nil {
		return "", err
	}
	now := c.now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase, id), nil)
	if err != nil {
		return "", err
	}
	setHeaders(req, jwtStr)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return "", RateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		// Never echo a token-mint body.
		return "", fmt.Errorf("github installation token: unexpected status %d", resp.StatusCode)
	}
	var p struct {
		Token       string            `json:"token"`
		ExpiresAt   string            `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return "", errors.New("github installation token: malformed response")
	}
	if p.Token == "" {
		return "", errors.New("github installation token: empty token")
	}
	exp := now.Add(time.Hour)
	if t, err := time.Parse(time.RFC3339, p.ExpiresAt); err == nil {
		exp = t
	}
	c.mu.Lock()
	c.tokens[id] = token{value: p.Token, expiry: exp, permissions: p.Permissions}
	c.mu.Unlock()
	return p.Token, nil
}

// Permissions returns what the installation granted its token (minting one if
// needed), e.g. {"contents":"write","pull_requests":"write"}.
func (c *Client) Permissions(ctx context.Context, installationID int64) (map[string]string, error) {
	if _, err := c.Token(ctx, installationID); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]string{}
	for k, v := range c.tokens[installationID].permissions {
		out[k] = v
	}
	return out, nil
}

// InstallationFor returns the installation id of owner/repo (GET
// /repos/{owner}/{repo}/installation as the App), cached for an hour.
func (c *Client) InstallationFor(ctx context.Context, owner, repo string) (int64, error) {
	if !c.Enabled() {
		return 0, ErrDisabled
	}
	k := strings.ToLower(owner + "/" + repo)
	c.mu.Lock()
	e, ok := c.insts[k]
	c.mu.Unlock()
	if ok && c.now().Sub(e.fetched) < installationTTL {
		return e.id, nil
	}
	v, err, _ := c.sf.Do("inst:"+k, func() (any, error) {
		jwtStr, err := c.JWT()
		if err != nil {
			return int64(0), err
		}
		var out struct {
			ID int64 `json:"id"`
		}
		if err := c.send(ctx, 0, jwtStr, http.MethodGet, "/repos/"+owner+"/"+repo+"/installation", nil, &out); err != nil {
			return int64(0), err
		}
		c.mu.Lock()
		c.insts[k] = instEntry{id: out.ID, fetched: c.now()}
		c.mu.Unlock()
		return out.ID, nil
	})
	if err != nil {
		return 0, err
	}
	return v.(int64), nil
}

func setHeaders(req *http.Request, bearer string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+bearer)
}

// ScopedToken is a short-lived installation token narrowed to one repository
// and a fixed permission set. It is minted fresh, never cached or logged.
type ScopedToken struct {
	Token     string
	ExpiresAt time.Time
}

// MintScoped mints an installation token limited to repo (name only, no
// owner) and perms, e.g. {"contents":"write","pull_requests":"write"}. GitHub
// refuses a permission the installation was not granted.
func (c *Client) MintScoped(ctx context.Context, installationID int64, repo string, perms map[string]string) (ScopedToken, error) {
	if !c.Enabled() {
		return ScopedToken{}, ErrDisabled
	}
	jwtStr, err := c.JWT()
	if err != nil {
		return ScopedToken{}, err
	}
	reqBody, err := json.Marshal(map[string]any{"repositories": []string{repo}, "permissions": perms})
	if err != nil {
		return ScopedToken{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase, installationID), bytes.NewReader(reqBody))
	if err != nil {
		return ScopedToken{}, err
	}
	setHeaders(req, jwtStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ScopedToken{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		// Never echo a token-mint body.
		return ScopedToken{}, fmt.Errorf("github scoped installation token: unexpected status %d", resp.StatusCode)
	}
	var p struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.Token == "" {
		return ScopedToken{}, errors.New("github scoped installation token: malformed response")
	}
	exp := c.now().Add(time.Hour)
	if t, err := time.Parse(time.RFC3339, p.ExpiresAt); err == nil {
		exp = t
	}
	return ScopedToken{Token: p.Token, ExpiresAt: exp}, nil
}
