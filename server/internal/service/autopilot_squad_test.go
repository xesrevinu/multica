package service

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotSquadAttribution(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	id = "01234567-8901-2345-6789-012345678901"

	tests := []struct {
		name string
		ap   db.Autopilot
		want string
	}{
		{"agent assignee returns zero", db.Autopilot{AssigneeType: "agent", AssigneeID: id}, ""},
		{"squad assignee returns squad id", db.Autopilot{AssigneeType: "squad", AssigneeID: id}, id},
		{"squad with invalid id returns zero", db.Autopilot{AssigneeType: "squad", AssigneeID: ""}, ""},
		{"unset type defaults to non-squad", db.Autopilot{AssigneeID: id}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := autopilotSquadAttribution(tc.ap)
			if (got != "") != (tc.want != "") {
				t.Fatalf("Valid mismatch: got %v want %v", (got != ""), (tc.want != ""))
			}
			if (got != "") && got != tc.want {
				t.Fatalf("Bytes mismatch")
			}
		})
	}
}

func TestFormatAdmissionReason(t *testing.T) {
	tests := []struct {
		name string
		ap   db.Autopilot
		raw  string
		want string
	}{
		{"agent archived", db.Autopilot{AssigneeType: "agent"}, "agent is archived", "assignee agent is archived"},
		{"squad archived", db.Autopilot{AssigneeType: "squad"}, "agent is archived", "squad leader agent is archived"},
		{"agent no runtime", db.Autopilot{AssigneeType: "agent"}, "agent has no runtime bound", "assignee agent has no runtime bound"},
		{"squad no runtime", db.Autopilot{AssigneeType: "squad"}, "agent has no runtime bound", "squad leader agent has no runtime bound"},
		{"runtime offline retains MUL-1899 suffix", db.Autopilot{AssigneeType: "agent"}, "agent runtime is offline", "agent runtime is offline at dispatch time"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatAdmissionReason(tc.ap, tc.raw); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// errDispatchSkipped must be distinguishable via errors.As from a wrapped
// fmt.Errorf, otherwise DispatchAutopilot's failure-vs-skip switch will treat
// it as a generic failure and the manual-trigger handler will 500. Locks in
// the contract that fixed the post-admission race (PR #2888 review fix #2).
func TestErrDispatchSkippedUnwraps(t *testing.T) {
	base := &errDispatchSkipped{reason: "squad leader agent is archived"}
	wrapped := fmt.Errorf("dispatch run_only: %w", base)

	var got *errDispatchSkipped
	if !errors.As(wrapped, &got) {
		t.Fatalf("errors.As did not match errDispatchSkipped through fmt.Errorf wrap")
	}
	if got.reason != base.reason {
		t.Fatalf("reason mismatch: got %q want %q", got.reason, base.reason)
	}

	// sql.ErrNoRows must NOT pass through the same gate — otherwise transient
	// "row not found" errors that should fail-open via shouldSkipDispatch
	// would be swallowed silently as skips at the dispatch level.
	if errors.As(sql.ErrNoRows, &got) {
		t.Fatal("sql.ErrNoRows wrongly satisfied errDispatchSkipped")
	}
}
