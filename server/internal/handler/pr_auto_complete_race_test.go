package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

)

// hookedTxStarter runs hook before starting a transaction, to schedule work
// inside a handler's decision→write window.
type hookedTxStarter struct {
	base txStarter
	hook func() error
}

func (h hookedTxStarter) BeginTx(ctx context.Context, opts *sql.TxOptions) (handlerTx, error) {
	if err := h.hook(); err != nil {
		return nil, err
	}
	return h.base.BeginTx(ctx, opts)
}

// A PR linked between the auto-complete decision and the status write must
// keep the issue open: the write re-checks the linked PRs (MUL-7429 review).

// fireTimedPRWebhook delivers a pull_request event that closes identifier,
// with an explicit updated_at.
func fireTimedPRWebhook(t *testing.T, inst int64, identifier, state, timestamp string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"action":       "edited",
		"installation": map[string]any{"id": inst},
		"repository":   map[string]any{"name": "widget", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{"number": 1, "html_url": "https://github.com/acme/widget/pull/1", "title": "Closes " + identifier, "state": state, "merged": state == "closed", "created_at": "2026-09-22T00:00:00Z", "updated_at": timestamp, "head": map[string]any{"ref": "fix/a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	testHandler.handlePullRequestEvent(context.Background(), raw)
}

// GitHub can deliver an old event late. It must not roll a merged PR back to
// open, which would make a redelivered merge look new and complete an issue a
// person reopened (MUL-7429 review).
