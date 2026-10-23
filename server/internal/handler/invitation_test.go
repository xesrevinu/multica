package handler

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const invitationTestEmail = "invitation-test@multica.ai"

type stubSeatCapacity struct {
	reserveDecision seatcapacity.Decision
	reserveErr      error
	reserveCalls    int
	releaseCalls    int
	consumeCalls    int
	consumeDecision *seatcapacity.Decision
	consumeErr      error
}

func (*stubSeatCapacity) RecoveryAvailable() bool { return true }

func (s *stubSeatCapacity) ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (seatcapacity.Decision, error) {
	s.reserveCalls++
	return s.reserveDecision, s.reserveErr
}
func (s *stubSeatCapacity) ClaimShareJoin(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) Consume(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	s.consumeCalls++
	decision := seatcapacity.Decision{Managed: true, Allowed: true}
	if s.consumeDecision != nil {
		decision = *s.consumeDecision
	}
	return decision, s.consumeErr
}
func (s *stubSeatCapacity) Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) Release(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	s.releaseCalls++
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *stubSeatCapacity) GetOperation(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{}, nil
}

func useSeatCapacity(t *testing.T, executor seatcapacity.Executor) {
	t.Helper()
	previous := testHandler.SeatCapacity
	testHandler.SeatCapacity = executor
	t.Cleanup(func() {
		testHandler.SeatCapacity = previous
	})
}

type stubInvitationRateLimiter struct {
	allowed     bool
	allowResult *bool
	err         error
	allowErr    error
	retryAfter  time.Duration
	checkKeys   []string
	allowKeys   []string
}

func (l *stubInvitationRateLimiter) Allow(ctx context.Context, key string) bool {
	allowed, _ := l.AllowWithError(ctx, key)
	return allowed
}

func (l *stubInvitationRateLimiter) AllowWithError(_ context.Context, key string) (bool, error) {
	l.allowKeys = append(l.allowKeys, key)
	if l.allowResult != nil {
		return *l.allowResult, l.allowErr
	}
	return l.allowed, l.allowErr
}

func (l *stubInvitationRateLimiter) Check(ctx context.Context, key string) bool {
	allowed, _ := l.CheckWithError(ctx, key)
	return allowed
}

func (l *stubInvitationRateLimiter) CheckWithError(_ context.Context, key string) (bool, error) {
	l.checkKeys = append(l.checkKeys, key)
	return l.allowed, l.err
}

func (l *stubInvitationRateLimiter) RetryAfter(_ context.Context, _ string) time.Duration {
	return l.retryAfter
}

func useInvitationRateLimiters(t *testing.T, limiters InvitationRateLimiters) {
	t.Helper()
	previous := testHandler.InvitationRateLimiters
	testHandler.InvitationRateLimiters = limiters
	t.Cleanup(func() {
		testHandler.InvitationRateLimiters = previous
	})
}

func clearInvitationsForTestWorkspace(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`DELETE FROM workspace_invitation WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID),
	); err != nil {
		t.Fatalf("clear invitations: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace_invitation WHERE workspace_id = $1`,
			parseUUID(testWorkspaceID),
		)
	})
}

type rollbackOnCommitTx struct {
	*sql.Tx
}

func (tx rollbackOnCommitTx) Commit() error {
	_ = tx.Tx.Rollback()
	return errors.New("forced commit failure")
}

type rollbackOnCommitTxStarter struct {
	pool *testutil.Pool
}

func (s rollbackOnCommitTxStarter) BeginTx(ctx context.Context, _ *sql.TxOptions) (handlerTx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return rollbackOnCommitTx{Tx: tx}, nil
}

// Sanity check: a fresh, live pending invitation must block re-invitation.





// Regression for issue #2055: an expired pending invitation must NOT block a
// new invitation to the same email. The stale row should be flipped to
// 'expired' and a fresh pending row should be created.








// An installed client can still show a pending-invitation row the user
// already accepted from another surface (web invite page, another device).
// Re-accept must be idempotent for the membership the first accept created,
// so the client's refetch drops the stale row instead of sticking forever.

// A concluded invitation whose membership is gone (the user left the
// workspace afterwards) must not be re-acceptable: leaving was explicit.

// Declining a concluded invitation is a no-op: the row only lingers in
// clients that concluded the same invitation elsewhere, and a 204 lets their
// refetch drop it.
