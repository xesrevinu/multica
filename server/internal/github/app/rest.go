package app

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APIError is a non-2xx REST response. Message is GitHub's "message" field.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("github: %d %s", e.Status, e.Message) }

// IsStatus reports whether err is an *APIError with the given status.
func IsStatus(err error, status int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == status
}

// RateLimitError means GitHub asked us to back off (primary or secondary
// limit), or the client already knows the budget is spent until the reset.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github rate limited, retry after %s", e.RetryAfter)
}

// RateState is the last X-RateLimit-* view of one installation.
type RateState struct {
	Limit     int
	Remaining int
	Reset     time.Time
	Seen      time.Time
}

// Low reports whether less than 10% of the hourly budget is left (and the
// window has not reset yet). Callers use it to fall back to cached /
// webhook-fed data instead of polling.
func (r RateState) Low(now time.Time) bool {
	if r.Seen.IsZero() || r.Limit <= 0 || !now.Before(r.Reset) {
		return false
	}
	return r.Remaining*10 < r.Limit
}

// Rate returns the last seen rate-limit state of an installation.
func (c *Client) Rate(installationID int64) RateState {
	if c == nil {
		return RateState{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rate[installationID]
}

// Do performs one REST call as the installation and decodes a JSON response
// into out (out may be nil). path is relative to the API base
// ("/repos/o/r/pulls/1"). GETs are conditional: a cached ETag is sent as
// If-None-Match and a 304 (free against the rate limit) replays the cached
// body. When the installation's budget is known to be spent, Do returns a
// *RateLimitError without calling GitHub.
func (c *Client) Do(ctx context.Context, installationID int64, method, path string, body, out any) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	if rs := c.Rate(installationID); !rs.Seen.IsZero() && rs.Remaining <= 0 && c.now().Before(rs.Reset) {
		return &RateLimitError{RetryAfter: clampWait(rs.Reset.Sub(c.now()))}
	}
	tok, err := c.Token(ctx, installationID)
	if err != nil {
		return err
	}
	err = c.send(ctx, installationID, tok, method, path, body, out)
	if IsStatus(err, http.StatusUnauthorized) {
		// Revoked or rotated token: mint once more and retry.
		c.Invalidate(installationID)
		if tok, err = c.Token(ctx, installationID); err != nil {
			return err
		}
		err = c.send(ctx, installationID, tok, method, path, body, out)
	}
	return err
}

func (c *Client) send(ctx context.Context, inst int64, bearer, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, rd)
	if err != nil {
		return err
	}
	setHeaders(req, bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	key := ""
	if method == http.MethodGet && inst != 0 {
		key = strconv.FormatInt(inst, 10) + " " + path
		if e, ok := c.etags.get(key); ok {
			req.Header.Set("If-None-Match", e.etag)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if inst != 0 {
		c.noteRate(inst, resp)
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && key != "":
		e, ok := c.etags.get(key)
		if !ok {
			return errors.New("github: 304 without a cached body")
		}
		raw = e.body
	case isRateLimited(resp, raw):
		return RateLimitFromResponse(resp, c.now())
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &m)
		return &APIError{Status: resp.StatusCode, Message: m.Message}
	default:
		// Writes need no invalidation: a later conditional GET of a changed
		// resource gets a 200 with the new body and ETag.
		if key != "" {
			if et := resp.Header.Get("ETag"); et != "" {
				c.etags.put(key, etagEntry{etag: et, body: raw})
			}
		}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("github: malformed response")
		}
	}
	return nil
}

// isRateLimited spots primary (Remaining: 0) and secondary (Retry-After or
// the documented message) limits; a plain 403 permission error is not one.
func isRateLimited(resp *http.Response, raw []byte) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "" {
			return true
		}
		return bytes.Contains(bytes.ToLower(raw), []byte("secondary rate limit"))
	}
	return false
}

func (c *Client) noteRate(inst int64, resp *http.Response) {
	rem := resp.Header.Get("X-RateLimit-Remaining")
	if rem == "" {
		return
	}
	r, err := strconv.Atoi(rem)
	if err != nil {
		return
	}
	st := RateState{Remaining: r, Seen: c.now()}
	st.Limit, _ = strconv.Atoi(resp.Header.Get("X-RateLimit-Limit"))
	if u, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		st.Reset = time.Unix(u, 0)
	}
	c.mu.Lock()
	c.rate[inst] = st
	c.mu.Unlock()
}

// RateLimitFromResponse derives the wait from Retry-After (seconds), else
// X-RateLimit-Reset (unix seconds), else 60s; clamped to [1s, 5m].
func RateLimitFromResponse(resp *http.Response, now time.Time) *RateLimitError {
	wait := time.Minute
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			wait = time.Duration(secs) * time.Second
		}
	} else if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			if d := time.Unix(unix, 0).Sub(now); d > 0 {
				wait = d
			}
		}
	}
	return &RateLimitError{RetryAfter: clampWait(wait)}
}

func clampWait(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	if d > 5*time.Minute {
		return 5 * time.Minute
	}
	return d
}

// GraphQL runs one query as the installation and returns the raw "data".
// GitHub answers 200 for query-level errors, so the errors array is checked
// too (RATE_LIMITED → *RateLimitError).
func (c *Client) GraphQL(ctx context.Context, installationID int64, query string, variables map[string]any) (json.RawMessage, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	tok, err := c.Token(ctx, installationID)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/graphql", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	setHeaders(req, tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, RateLimitFromResponse(resp, c.now())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github graphql: unexpected status %d", resp.StatusCode)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, errors.New("github graphql: malformed response")
	}
	if len(env.Errors) > 0 {
		for _, e := range env.Errors {
			if e.Type == "RATE_LIMITED" {
				return nil, &RateLimitError{RetryAfter: time.Minute}
			}
		}
		return nil, fmt.Errorf("github graphql error: %s", env.Errors[0].Message)
	}
	if len(env.Data) == 0 {
		return nil, errors.New("github graphql: empty data")
	}
	return env.Data, nil
}

// etagCache is a small LRU of conditional-request bodies.
type etagCache struct {
	mu  sync.Mutex
	cap int
	ll  *list.List
	m   map[string]*list.Element
}

type etagEntry struct {
	key  string
	etag string
	body []byte
}

func newETagCache(n int) *etagCache {
	if n <= 0 {
		n = 1024
	}
	return &etagCache{cap: n, ll: list.New(), m: map[string]*list.Element{}}
}

func (c *etagCache) get(k string) (etagEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[k]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(etagEntry), true
	}
	return etagEntry{}, false
}

func (c *etagCache) put(k string, e etagEntry) {
	e.key = k
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[k]; ok {
		el.Value = e
		c.ll.MoveToFront(el)
		return
	}
	c.m[k] = c.ll.PushFront(e)
	for c.ll.Len() > c.cap {
		old := c.ll.Back()
		c.ll.Remove(old)
		delete(c.m, old.Value.(etagEntry).key)
	}
}
