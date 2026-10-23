package handler

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type metadataMutationResponse struct {
	Metadata      map[string]any `json:"metadata"`
	IssueRevision int64          `json:"issue_revision"`
}

type metadataRowState struct {
	ctid           string
	xmin           string
	metadata       string
	revision       int64
	updatedAt      string
	lastActivityAt string
}

func readMetadataRowState(t *testing.T, issueID string) metadataRowState {
	t.Helper()
	var state metadataRowState
	dbfx.QueryRow(t, `
		SELECT ctid::text, xmin::text, metadata, revision,
		       updated_at::text, COALESCE(last_activity_at::text, '<null>')
		FROM issue WHERE id = $1
	`, issueID).Scan(
		&state.ctid,
		&state.xmin,
		&state.metadata,
		&state.revision,
		&state.updatedAt,
		&state.lastActivityAt,
	)
	return state
}

func mutationRequest(method, issueID, key string, value any) *http.Request {
	return testutil.WithURLParams(
		newRequest(method, "/api/issues/"+issueID+"/metadata/"+key, value),
		"id", issueID,
		"key", key,
	)
}

func handlerWithMetadataEventCounter(counter *atomic.Int64) *Handler {
	h := *testHandler
	h.Bus = events.New()
	h.Bus.Subscribe(protocol.EventIssueMetadataChanged, func(events.Event) {
		counter.Add(1)
	})
	return &h
}


