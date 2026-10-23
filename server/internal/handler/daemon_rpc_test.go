package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemonws"
)

// TestDaemonRPCHandler_TasksClaim pins the WS-first claim binding (MUL-4257):
// a tasks.claim RPC, driven with the WS connection's identity, reuses the HTTP
// claim handler and claims a queued task for the daemon's runtime.

// TestDaemonRPCHandler_UnknownMethod returns 404 for an unknown method.
func TestDaemonRPCHandler_UnknownMethod(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	status, _, err := testHandler.DaemonRPCHandler(context.Background(),
		daemonws.ClientIdentity{DaemonID: "ws-daemon"}, "does.not.exist", nil)
	if status != http.StatusNotFound || err == nil {
		t.Fatalf("status=%d err=%v, want 404 + error", status, err)
	}
}
