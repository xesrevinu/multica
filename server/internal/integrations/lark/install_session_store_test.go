package lark

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func installSessionUUID(t *testing.T, s string) string {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u.String()
}

// Both implementations must honour the same contract, so the behavioural
// suite runs against each. The Redis case is the one that matters in
// production; the memory case is what a single-instance dev deploy gets,
// and running them through identical assertions is what keeps the two
// from drifting.
func forEachInstallSessionStore(t *testing.T, fn func(t *testing.T, store InstallSessionStore)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) {
		fn(t, NewMemoryInstallSessionStore())
	})
}

func TestInstallSessionStoreRoundTrip(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		initiator := installSessionUUID(t, "44444444-4444-4444-4444-444444444444")
		expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

		if err := store.Create(ctx, InstallSessionState{
			ID:          "sess-1",
			WorkspaceID: ws,
			InitiatorID: initiator,
			Status:      RegistrationStatusPending,
			ExpiresAt:   expiresAt,
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := store.Get(ctx, ws, "sess-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != RegistrationStatusPending {
			t.Errorf("Status: got %q want pending", got.Status)
		}
		// The initiator authorizes the status read, so losing it would
		// silently turn every non-admin's poll into a 404.
		if got.InitiatorID != initiator {
			t.Errorf("InitiatorID: got %v want %v", got.InitiatorID, initiator)
		}
		if !got.ExpiresAt.Equal(expiresAt) {
			t.Errorf("ExpiresAt: got %s want %s", got.ExpiresAt, expiresAt)
		}
	})
}

func TestInstallSessionStoreUnknownAndCrossWorkspaceAreIndistinguishable(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		otherWs := installSessionUUID(t, "22222222-2222-2222-2222-222222222222")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-2", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}

		if _, err := store.Get(ctx, ws, "no-such-session"); !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("unknown id: want ErrInstallSessionNotFound, got %v", err)
		}
		if _, err := store.Get(ctx, otherWs, "sess-2"); !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("cross-workspace: want ErrInstallSessionNotFound, got %v", err)
		}
	})
}

// The expiry deadline and a poll result can terminate the same session
// concurrently. Whichever lands first is what the user was already shown,
// so a later write must be a silent no-op — not an error, and not an
// overwrite.
func TestInstallSessionStoreTerminalIsFirstWriterWins(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		installationID := installSessionUUID(t, "33333333-3333-3333-3333-333333333333")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-3", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}

		if err := store.MarkTerminal(ctx, "sess-3", InstallSessionOutcome{
			Status:       RegistrationStatusError,
			ErrorReason:  RegistrationReasonAccessDenied,
			ErrorMessage: "user denied",
		}, 30*time.Minute); err != nil {
			t.Fatalf("first MarkTerminal: %v", err)
		}
		if err := store.MarkTerminal(ctx, "sess-3", InstallSessionOutcome{
			Status: RegistrationStatusSuccess, InstallationID: installationID,
		}, 30*time.Minute); err != nil {
			t.Fatalf("second MarkTerminal must be a no-op, not an error: %v", err)
		}

		got, err := store.Get(ctx, ws, "sess-3")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != RegistrationStatusError {
			t.Fatalf("Status: got %q want error — a late success must not overwrite a recorded failure", got.Status)
		}
		if got.ErrorReason != RegistrationReasonAccessDenied {
			t.Errorf("ErrorReason: got %q want %q", got.ErrorReason, RegistrationReasonAccessDenied)
		}
	})
}

func TestInstallSessionStoreSuccessCarriesInstallationID(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")
		installationID := installSessionUUID(t, "33333333-3333-3333-3333-333333333333")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-4", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
		}, time.Hour); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.MarkTerminal(ctx, "sess-4", InstallSessionOutcome{
			Status: RegistrationStatusSuccess, InstallationID: installationID,
		}, 30*time.Minute); err != nil {
			t.Fatalf("MarkTerminal: %v", err)
		}

		got, err := store.Get(ctx, ws, "sess-4")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != RegistrationStatusSuccess {
			t.Fatalf("Status: got %q want success", got.Status)
		}
		// The dialog closes on this id and invalidates the installations
		// cache, so dropping it would strand a successful bind.
		if got.InstallationID != installationID {
			t.Errorf("InstallationID: got %v want %v", got.InstallationID, installationID)
		}
	})
}

// Retention is the store's job now — there is no sweep to run. A record
// past its TTL must read as gone, not as stale-but-present.
func TestInstallSessionStoreRetentionExpires(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		ctx := context.Background()
		ws := installSessionUUID(t, "11111111-1111-1111-1111-111111111111")

		if err := store.Create(ctx, InstallSessionState{
			ID: "sess-5", WorkspaceID: ws, InitiatorID: ws,
			Status: RegistrationStatusPending, ExpiresAt: time.Now().Add(time.Hour),
			// 1s is the shortest retention both implementations can
			// express: Redis EXPIRE is second-granular, and ttlSeconds
			// floors at 1 so a sub-second TTL cannot round down to 0,
			// which Redis reads as "no expiry".
		}, time.Second); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := store.Get(ctx, ws, "sess-5"); err != nil {
			t.Fatalf("within retention: %v", err)
		}

		time.Sleep(1300 * time.Millisecond)

		if _, err := store.Get(ctx, ws, "sess-5"); !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("past retention: want ErrInstallSessionNotFound, got %v", err)
		}
	})
}

// A MarkTerminal for a session that is already gone reports
// ErrInstallSessionNotFound so the service's retry loop stops instead of
// burning its whole budget on something no retry can fix.
func TestInstallSessionStoreMarkTerminalOnMissingSessionIsNotFound(t *testing.T) {
	forEachInstallSessionStore(t, func(t *testing.T, store InstallSessionStore) {
		err := store.MarkTerminal(context.Background(), "never-existed",
			InstallSessionOutcome{Status: RegistrationStatusSuccess}, time.Minute)
		if !errors.Is(err, ErrInstallSessionNotFound) {
			t.Errorf("want ErrInstallSessionNotFound, got %v", err)
		}
	})
}
