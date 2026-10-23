package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/seatcapacity"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var testSeq atomic.Int64

type shareJoinCapacityStub struct {
	mu              sync.Mutex
	claimTokens     []uuid.UUID
	releaseTokens   []uuid.UUID
	claimGate       *sync.WaitGroup
	claimDecision   *seatcapacity.Decision
	releaseDecision *seatcapacity.Decision
}

func (*shareJoinCapacityStub) RecoveryAvailable() bool { return true }

func (s *shareJoinCapacityStub) ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) ClaimShareJoin(_ context.Context, _ uuid.UUID, token uuid.UUID) (seatcapacity.Decision, error) {
	s.mu.Lock()
	s.claimTokens = append(s.claimTokens, token)
	s.mu.Unlock()
	if s.claimGate != nil {
		s.claimGate.Done()
		s.claimGate.Wait()
	}
	decision := seatcapacity.Decision{Managed: true, Allowed: true}
	if s.claimDecision != nil {
		decision = *s.claimDecision
	}
	return decision, nil
}
func (s *shareJoinCapacityStub) Consume(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) Release(_ context.Context, _ uuid.UUID, token uuid.UUID) (seatcapacity.Decision, error) {
	s.mu.Lock()
	s.releaseTokens = append(s.releaseTokens, token)
	decision := seatcapacity.Decision{Managed: true, Allowed: true}
	if s.releaseDecision != nil {
		decision = *s.releaseDecision
	}
	s.mu.Unlock()
	return decision, nil
}
func (s *shareJoinCapacityStub) ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{Managed: true, Allowed: true}, nil
}
func (s *shareJoinCapacityStub) GetOperation(context.Context, uuid.UUID, uuid.UUID) (seatcapacity.Decision, error) {
	return seatcapacity.Decision{}, nil
}

func (s *shareJoinCapacityStub) tokens() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uuid.UUID(nil), s.claimTokens...)
}

func (s *shareJoinCapacityStub) releases() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uuid.UUID(nil), s.releaseTokens...)
}

func pgInt32(v int32) *int32 { return &v }

func clearShareLinksForTestWorkspace(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`DELETE FROM workspace_share_link WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID),
	); err != nil {
		t.Fatalf("clear share links: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM workspace_share_link WHERE workspace_id = $1`,
			parseUUID(testWorkspaceID),
		)
	})
}

// createTestUserAndMember inserts a fresh user and (optionally) a member row in
// the test workspace with the given role. Returns the new user id.
func createTestUserAndMember(t *testing.T, role string) string {
	t.Helper()
	ctx := context.Background()
	seq := testSeq.Add(1)
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("sharelink-test-%d", seq),
		fmt.Sprintf("sharelink-test-%d@multica.ai", seq),
	).Scan(&userID); err != nil {
		t.Fatalf("create share-link test user: %v", err)
	}
	if role != "" {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
			parseUUID(testWorkspaceID), parseUUID(userID), role,
		); err != nil {
			t.Fatalf("create share-link test member: %v", err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, parseUUID(userID))
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, parseUUID(userID))
	})
	return userID
}

// createTestShareLink inserts a share link directly and returns its code.
func createTestShareLink(t *testing.T, workspaceID, role string, maxUses int32, expireHours int32) db.WorkspaceShareLink {
	t.Helper()
	ctx := context.Background()
	var link db.WorkspaceShareLink
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_share_link (workspace_id, code, created_by, role, max_uses, expires_at)
		VALUES ($1, $2, $3, $4,
		        CASE WHEN $5 = 0 THEN NULL ELSE $5 END,
		        CASE WHEN $6 = 0 THEN NULL ELSE now() + ($6 * interval '1 hour') END)
		RETURNING id, workspace_id, code, created_by, role, expires_at, max_uses, use_count, is_active, created_at
	`,
		parseUUID(workspaceID),
		fmt.Sprintf("testcode-%d", testSeq.Add(1)),
		parseUUID(testUserID),
		role,
		pgInt32(maxUses),
		pgInt32(expireHours),
	).Scan(&link.ID, &link.WorkspaceID, &link.Code, &link.CreatedBy, &link.Role, &link.ExpiresAt, &link.MaxUses, &link.UseCount, &link.IsActive, &link.CreatedAt)
	if err != nil {
		t.Fatalf("create test share link: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_share_link WHERE id = $1`, link.ID)
	})
	return link
}

// --- Tests ---

func TestCreateShareLink_RequiresOwnerAdmin(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	r := chi.NewRouter()
	r.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
		r.Post("/share-links", testHandler.CreateShareLink)
	})

	// Owner succeeds.
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member is denied.
	memberID := createTestUserAndMember(t, "member")
	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
	req2.Header.Set("X-User-ID", memberID)
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("member create: expected 403, got %d: %s", w2.Code, w2.Body.String())
	}
}



func TestCreateShareLink_RejectsInvalidInput(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// Negative expires_in is rejected.
	neg := -1
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", ExpiresIn: &neg})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateShareLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative expires_in: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Zero max_uses is rejected.
	zero := 0
	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", MaxUses: &zero})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	testHandler.CreateShareLink(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("zero max_uses: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateShareLink_RejectsOverflowingInput(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// max_uses above math.MaxInt32 wraps in int32(...) — must be rejected.
	overMax := 2147483648 // MaxInt32 + 1
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", MaxUses: &overMax})
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.CreateShareLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("max_uses over MaxInt32: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// expires_in above the duration-safe bound overflows time.Duration * time.Hour.
	overExp := int(expiresInMaxHours) + 1
	req2 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", ExpiresIn: &overExp})
	req2 = withURLParam(req2, "id", testWorkspaceID)
	w2 := httptest.NewRecorder()
	testHandler.CreateShareLink(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expires_in over duration-safe bound: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}

	// Boundary: expires_in == expiresInMaxHours must be accepted, and the
	// stored expires_at must be strictly in the future (not wrapped negative).
	maxExp := int(expiresInMaxHours)
	req3 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", ExpiresIn: &maxExp})
	req3 = withURLParam(req3, "id", testWorkspaceID)
	w3 := httptest.NewRecorder()
	testHandler.CreateShareLink(w3, req3)
	if w3.Code != http.StatusCreated {
		t.Fatalf("expires_in = duration-safe max: expected 201, got %d: %s", w3.Code, w3.Body.String())
	}
	var expiresAt string
	if err := testPool.QueryRow(context.Background(),
		`SELECT expires_at FROM workspace_share_link WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 1`,
		parseUUID(testWorkspaceID)).Scan(&expiresAt); err != nil {
		t.Fatalf("load created link expiry: %v", err)
	}
	if !(expiresAt != "") || !parseDBTime(expiresAt).After(time.Now()) {
		t.Fatalf("accepted max expires_in must persist a future expires_at, got %v", parseDBTime(expiresAt))
	}

	// Deactivate so a fresh active link can be created for the max_uses check.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace_share_link SET is_active = false WHERE workspace_id = $1`,
		parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("deactivate for max_uses boundary: %v", err)
	}
	maxUse := 2147483647
	req4 := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member", MaxUses: &maxUse})
	req4 = withURLParam(req4, "id", testWorkspaceID)
	w4 := httptest.NewRecorder()
	testHandler.CreateShareLink(w4, req4)
	if w4.Code != http.StatusCreated {
		t.Fatalf("max_uses = MaxInt32: expected 201, got %d: %s", w4.Code, w4.Body.String())
	}
}






// pendingInvitationForShareJoiner inserts a pending email invitation for a
// share-link join test user and returns its id. role is the invitation's own
// role, which the join must NOT propagate to the member.
func pendingInvitationForShareJoiner(t *testing.T, userID, email, role string) string {
	t.Helper()
	return dbfx.Insert(t, "workspace_invitation", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"inviter_id":      testUserID,
		"invitee_email":   email,
		"invitee_user_id": userID,
		"role":            role,
		"status":          "pending",
		"expires_at":      testutil.Raw("now() + interval '1 day'"),
	})
}

func shareJoinerEmail(t *testing.T, userID string) string {
	t.Helper()
	var email string
	if err := testPool.QueryRow(context.Background(),
		`SELECT email FROM "user" WHERE id = $1`, parseUUID(userID),
	).Scan(&email); err != nil {
		t.Fatalf("load share-join test user email: %v", err)
	}
	return email
}

func invitationStatusOf(t *testing.T, invitationID string) (string, string) {
	t.Helper()
	var status, role string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, role FROM workspace_invitation WHERE id = $1`, parseUUID(invitationID),
	).Scan(&status, &role); err != nil {
		t.Fatalf("reload invitation: %v", err)
	}
	return status, role
}

// A user invited by email who joins through the share link instead must have
// that pending invitation settled in the same transaction: otherwise the row
// keeps holding its seat reservation, keeps showing in their pending list,
// and a later Accept on it dies on the CreateMember 409. The link's role
// wins — the user chose this entry point — and the invitation row keeps its
// own role, only its status is settled.

// The settled invitation signals its conclusion on the same channel the
// accept path uses, so workspace admins refresh their pending-invitation
// list instead of holding a row that is already over.

// In managed capacity mode the settled invitation was holding a seat
// reservation keyed by the invitation id; the join's own confirmed claim now
// covers the seat, so the reservation must be released — otherwise the same
// user holds a member seat and an invitation reservation at once.

// Unmanaged capacity keeps no durable reservation for an invitation, so the
// settle must still succeed end to end and leave nothing stranded in the
// outbox.

// A share-link join for a user without a pending invitation proceeds as
// before — the settle lookup misses and changes nothing.


func TestShareLink_CrossWorkspaceDenied(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// A user who is a member of another workspace must not list/create links
	// in the test workspace. Create a member in a throwaway workspace.
	ctx := context.Background()
	var otherWS string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ($1, $2) RETURNING id`,
		"ShareLink Other", fmt.Sprintf("sharelink-other-%d", testSeq.Add(1)),
	).Scan(&otherWS); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, parseUUID(otherWS))
	})

	otherUser := createTestUserAndMember(t, "")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		parseUUID(otherWS), parseUUID(otherUser),
	); err != nil {
		t.Fatalf("add owner to other workspace: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
		r.Post("/share-links", testHandler.CreateShareLink)
	})

	// otherUser is not a member of the test workspace -> must be denied.
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
	req.Header.Set("X-User-ID", otherUser)
	req = withURLParam(req, "id", testWorkspaceID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace create: expected 403 or 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateShareLink_ConcurrentCreatesSingleActive(t *testing.T) {
	clearShareLinksForTestWorkspace(t)

	// Concurrent creates on the same workspace: the partial unique index
	// (workspace_id WHERE is_active) serializes them so that at most one active
	// link exists at any time — later creates deactivate the earlier one rather
	// than both succeeding. Assert the invariant: exactly one active row.
	const n = 3
	results := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/share-links", CreateShareLinkRequest{Role: "member"})
			req = withURLParam(req, "id", testWorkspaceID)
			w := httptest.NewRecorder()
			testHandler.CreateShareLink(w, req)
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)

	for code := range results {
		if code != http.StatusCreated && code != http.StatusConflict && code != http.StatusInternalServerError {
			t.Fatalf("unexpected create status: %d", code)
		}
	}

	var active int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM workspace_share_link WHERE workspace_id = $1 AND is_active = true`,
		parseUUID(testWorkspaceID),
	).Scan(&active); err != nil {
		t.Fatalf("count active links: %v", err)
	}
	if active != 1 {
		t.Fatalf("concurrent creates: expected exactly 1 active link, got %d", active)
	}
}
