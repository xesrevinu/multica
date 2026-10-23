package handler

import (
)

// Neither workspace_mcp_server nor agent_mcp_server carries a foreign key
// (repo rule), so nothing in the database catches a write that lands after the
// row it depends on is gone. These two tests drive the actual races with two
// concurrent transactions rather than asserting the sequential outcome, which
// is what a lock protocol has to be tested against.
//
// They reuse the fence-test helpers from workspace_delete_fence_test.go:
// waitForBlockedWriter proves the second transaction is genuinely parked on a
// lock instead of merely being slow.

// A create arriving mid-teardown must not leave a library row pointing at a
// workspace that no longer exists.

// An assignment arriving while a library entry is being deleted must not leave
// a binding to a server that no longer exists.
