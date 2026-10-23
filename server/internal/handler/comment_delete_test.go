package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Deleting a comment removes only that comment (#8296). These tests pin the
// three outcomes: a comment with replies becomes a tombstone that keeps them
// attached, a comment without replies is removed, and a tombstone is removed
// once its last reply is gone.

type commentRowState struct {
	exists   bool
	content  string
	parentID string
	deleted  bool
	resolved bool
}

func loadCommentRowState(t *testing.T, id string) commentRowState {
	t.Helper()
	var state commentRowState
	var parentID *string
	var deletedAt, resolvedAt *time.Time
	err := testPool.QueryRow(context.Background(),
		`SELECT content, parent_id::text, deleted_at, resolved_at FROM comment WHERE id = $1`, id,
	).Scan(&state.content, &parentID, &deletedAt, &resolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return commentRowState{}
	}
	if err != nil {
		t.Fatalf("load comment %s: %v", id, err)
	}
	state.exists = true
	if parentID != nil {
		state.parentID = *parentID
	}
	state.deleted = deletedAt != nil
	state.resolved = resolvedAt != nil
	return state
}

// commentEventRecorder is a handler copy whose bus records the comment events
// a delete publishes, in order.
type commentEventRecorder struct {
	h      *Handler
	events []events.Event
}

func newCommentEventRecorder() *commentEventRecorder {
	h := *testHandler
	h.Bus = events.New()
	rec := &commentEventRecorder{h: &h}
	for _, eventType := range []string{protocol.EventCommentUpdated, protocol.EventCommentDeleted} {
		h.Bus.Subscribe(eventType, func(event events.Event) {
			rec.events = append(rec.events, event)
		})
	}
	return rec
}

func (rec *commentEventRecorder) delete(t *testing.T, commentID string) *testutil.Response {
	t.Helper()
	rec.events = nil
	req := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+commentID, nil), "commentId", commentID)
	return testutil.Call(t, rec.h.DeleteComment, req)
}

// deletedIDs returns the comment ids of the recorded comment:deleted events.
func (rec *commentEventRecorder) deletedIDs() []string {
	var ids []string
	for _, event := range rec.events {
		if event.Type == protocol.EventCommentDeleted {
			ids = append(ids, event.Payload.(map[string]any)["comment_id"].(string))
		}
	}
	return ids
}



// A tombstone carries no input: it must not count as a new comment for the
// claim hint, nor be replayed by completion reconciliation.

// A reply under a deleted agent comment continues the thread: it wakes neither
// the deleted comment's author nor, as a fallback, the issue assignee. The web
// reply box posts under the thread root, so this is every later reply once an
// agent-authored root with replies is deleted.

// A comment deleted after capture reads as removed, not as emptied; one that
// was already a tombstone when captured is not a change at all.


// recordingStorage records which stored objects a handler cleaned up.
type recordingStorage struct {
	mockStorage
	mu      sync.Mutex
	deleted []string
}

func (s *recordingStorage) DeleteKeys(_ context.Context, keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, keys...)
}

// A write to a comment's children that waited on the comment while a delete
// tombstoned it must not land on the tombstone. The holder transaction stands
// in for a delete that already holds the comment lock: the write blocks on
// LockLiveComment, the tombstone commits, and the write must be refused.

