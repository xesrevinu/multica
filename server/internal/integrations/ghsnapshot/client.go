// Package ghsnapshot fetches a GitHub pull request's CI + mergeability state
// from the GitHub API and treats that response as the single source of truth
// for the PR card (MUL-5265, Plan C). Webhooks and page visits only trigger a
// refresh; nothing here infers state incrementally from webhook payloads.
//
// The package has three layers:
//   - Client (this file): a thin view over the shared GitHub App client
//     (internal/github/app), which owns the App JWT, the per-installation
//     token cache, conditional requests and rate-limit tracking.
//   - snapshot.go: the single GraphQL query, contexts pagination, and
//     normalization into a flat per-check snapshot.
//   - refresh.go: the outbound work queue (dedup, single in-flight per PR,
//     bounded concurrency, Retry-After backoff), the head-SHA-guarded atomic
//     write, and the trigger surfaces (webhook / page visit / TTL sweep).
//
// Credential hygiene (acceptance criterion 6): the App private key and every
// installation token are treated as opaque secrets and are NEVER written to a
// log or embedded in an error message.
package ghsnapshot

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/github/app"
)

// RateLimitError signals that GitHub asked us to back off.
type RateLimitError = app.RateLimitError

// APIError is a non-2xx REST response.
type APIError = app.APIError

// Client is an installation-token-authenticated GitHub API client. A nil
// *Client is a valid "feature disabled" value — every method the refresh
// pipeline calls tolerates it — so a deployment without a GitHub App private
// key degrades cleanly (acceptance criterion 4).
type Client struct {
	app *app.Client
}

// NewClientFromEnv wraps the process-wide App client (app.Shared), so the
// snapshot pipeline, the PR actions and the orchestrator engine share one
// token cache and one rate-limit view.
//
//   - GITHUB_APP_ID / GITHUB_APP_PRIVATE_KEY unset → (nil, nil).
//   - Key present but malformed → (nil, err).
func NewClientFromEnv() (*Client, error) {
	a, err := app.Shared()
	if err != nil || a == nil {
		return nil, err
	}
	return &Client{app: a}, nil
}

// NewClient wraps an existing App client.
func NewClient(a *app.Client) *Client {
	if a == nil {
		return nil
	}
	return &Client{app: a}
}

// App returns the underlying App client (nil when disabled).
func (c *Client) App() *app.Client {
	if c == nil {
		return nil
	}
	return c.app
}

// Enabled reports whether the App API is configured. A nil client is disabled.
func (c *Client) Enabled() bool { return c != nil && c.app.Enabled() }

func (c *Client) installationToken(ctx context.Context, installationID int64) (string, error) {
	return c.app.Token(ctx, installationID)
}

func (c *Client) graphQL(ctx context.Context, installationID int64, query string, variables map[string]any) (json.RawMessage, error) {
	return c.app.GraphQL(ctx, installationID, query, variables)
}

func rateLimitFromResponse(resp *http.Response, now time.Time) *RateLimitError {
	return app.RateLimitFromResponse(resp, now)
}
