package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Response types ──────────────────────────────────────────────────────────

type SquadResponse struct {
	ID            string                       `json:"id"`
	WorkspaceID   string                       `json:"workspace_id"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	Instructions  string                       `json:"instructions"`
	AvatarURL     *string                      `json:"avatar_url"`
	LeaderID      string                       `json:"leader_id"`
	CreatorID     string                       `json:"creator_id"`
	CreatedAt     string                       `json:"created_at"`
	UpdatedAt     string                       `json:"updated_at"`
	ArchivedAt    *string                      `json:"archived_at"`
	ArchivedBy    *string                      `json:"archived_by"`
	MemberCount   int                          `json:"member_count"`
	MemberPreview []SquadMemberPreviewResponse `json:"member_preview"`
}

type SquadMemberPreviewResponse struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
}

type squadMemberSummary struct {
	count   int
	preview []SquadMemberPreviewResponse
}

type SquadMemberResponse struct {
	ID         string `json:"id"`
	SquadID    string `json:"squad_id"`
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
	CreatedAt  string `json:"created_at"`
}

// ── Converters ──────────────────────────────────────────────────────────────

func (h *Handler) squadToResponse(s db.Squad) SquadResponse {
	return SquadResponse{
		ID:            uuidToString(s.ID),
		WorkspaceID:   uuidToString(s.WorkspaceID),
		Name:          s.Name,
		Description:   s.Description,
		Instructions:  s.Instructions,
		AvatarURL:     h.resolveAvatarURLPtr(textToPtr(s.AvatarUrl)),
		LeaderID:      uuidToString(s.LeaderID),
		CreatorID:     uuidToString(s.CreatorID),
		CreatedAt:     timestampToString(s.CreatedAt),
		UpdatedAt:     timestampToString(s.UpdatedAt),
		ArchivedAt:    timestampToPtr(s.ArchivedAt),
		ArchivedBy:    uuidToPtr(s.ArchivedBy),
		MemberPreview: []SquadMemberPreviewResponse{},
	}
}

func squadMemberToResponse(m db.SquadMember) SquadMemberResponse {
	return SquadMemberResponse{
		ID:         uuidToString(m.ID),
		SquadID:    uuidToString(m.SquadID),
		MemberType: m.MemberType,
		MemberID:   uuidToString(m.MemberID),
		Role:       m.Role,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

func addSquadMemberPreview(summary *squadMemberSummary, memberType string, memberID pgtype.UUID, role string) {
	summary.count++
	if len(summary.preview) >= 3 {
		return
	}
	summary.preview = append(summary.preview, SquadMemberPreviewResponse{
		MemberType: memberType,
		MemberID:   uuidToString(memberID),
		Role:       role,
	})
}

func applySquadMemberSummary(resp *SquadResponse, summary *squadMemberSummary) {
	if summary == nil {
		return
	}
	resp.MemberCount = summary.count
	resp.MemberPreview = summary.preview
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// canManageSquad reports whether the member may mutate the squad. Workspace
// owner/admin manage every squad; a regular member manages only the squads
// they created. Squads stay creator-scoped for management while remaining
// visible workspace-wide (ListSquads is unfiltered). Mirrors the front-end
// per-squad `canManage` gate so the UI and API agree on who can rename / add
// members / archive (MUL-4223).
func canManageSquad(member db.Member, squad db.Squad) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	return uuidToString(squad.CreatorID) == uuidToString(member.UserID)
}

// memberCanWireAgent reports whether the acting member may attach the given
// agent to a squad (as leader or worker). Workspace owner/admin may wire any
// workspace agent — their management surface is unchanged. A regular member
// (a creator managing their own squad) may only wire agents they can
// @-trigger: canInvokeAgent judged as the member themselves, so public_to
// agents on their allow-list and their own private agents pass, while other
// members' private / non-allow-listed agents are rejected. This stops a
// creator from smuggling an agent they cannot invoke into a squad and reaching
// it through squad routing (MUL-4223).
func (h *Handler) memberCanWireAgent(ctx context.Context, member db.Member, agent db.Agent, workspaceID string) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	uid := uuidToString(member.UserID)
	return h.canInvokeAgent(ctx, agent, "member", uid, uid, workspaceID)
}

// loadSquadInWorkspace loads a squad scoped to the current workspace.
func (h *Handler) loadSquadInWorkspace(w http.ResponseWriter, r *http.Request) (db.Squad, string, bool) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	squadID := chi.URLParam(r, "id")
	squadUUID, ok := parseUUIDOrBadRequest(w, squadID, "squad id")
	if !ok {
		return db.Squad{}, "", false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.Squad{}, "", false
	}
	squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
		ID:          squadUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "squad not found")
		return db.Squad{}, "", false
	}
	return squad, workspaceID, true
}

func (h *Handler) loadSquadMemberSummary(ctx context.Context, squadID pgtype.UUID) (*squadMemberSummary, error) {
	rows, err := h.Queries.ListSquadMemberPreviewRowsBySquad(ctx, squadID)
	if err != nil {
		return nil, err
	}
	summary := &squadMemberSummary{}
	for _, row := range rows {
		addSquadMemberPreview(summary, row.MemberType, row.MemberID, row.Role)
	}
	return summary, nil
}

func (h *Handler) squadToResponseWithPreview(ctx context.Context, squad db.Squad) (SquadResponse, error) {
	resp := h.squadToResponse(squad)
	summary, err := h.loadSquadMemberSummary(ctx, squad.ID)
	if err != nil {
		return resp, err
	}
	applySquadMemberSummary(&resp, summary)
	return resp, nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

// ── Squad Members ───────────────────────────────────────────────────────────

// ── Squad Member Status ────────────────────────────────────────────────────

// SquadMemberStatus is the per-member entry in the squad member status
// response. Agent members carry a derived working/idle/offline/unstable
// status plus any active issues; human members are returned with member_type
// only so the front-end can render them in the same list without
// reordering.
type SquadMemberStatusResponse struct {
	MemberType   string                  `json:"member_type"`
	MemberID     string                  `json:"member_id"`
	Status       *string                 `json:"status"`
	ActiveIssues []SquadActiveIssueBrief `json:"active_issues"`
	LastActiveAt *string                 `json:"last_active_at"`
}

type SquadActiveIssueBrief struct {
	IssueID     string `json:"issue_id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	IssueStatus string `json:"issue_status"`
}

type SquadMemberStatusListResponse struct {
	Members []SquadMemberStatusResponse `json:"members"`
}

// deriveRuntimeAvailability owns the shared three-state runtime reachability
// policy. Agent projections use these values directly; squad presence maps an
// online runtime to its user-facing "idle" bucket after workload precedence.
func deriveRuntimeAvailability(
	runtimeStatus pgtype.Text,
	lastSeen pgtype.Timestamptz,
	now time.Time,
) string {
	if !runtimeStatus.Valid {
		return "offline"
	}
	if runtimeStatus.String == "online" {
		return "online"
	}
	if !lastSeen.Valid {
		return "offline"
	}
	if now.Sub(lastSeen.Time) < 5*time.Minute {
		return "unstable"
	}
	return "offline"
}

// deriveSquadMemberStatus collapses runtime + task signals into the five
// status buckets used by the squad UI. Mirrors the workload+availability
// split in packages/core/agents/derive-presence.ts: working wins over
// runtime health (an agent that is in the middle of dispatched/running
// work counts as working even if the runtime briefly drops), then
// availability buckets decide between idle / unstable / offline.
//
// Thresholds match deriveRuntimeHealth: any offline runtime whose
// last_seen_at is within the last 5 minutes is reported as "unstable" so
// the squad UI surfaces transient drops the same way the agent dot does.
//
// Archived agents always report `archived` regardless of any leftover
// runtime row or task — they should appear in the list but never look
// like they're still working or merely offline (a leftover online
// runtime row would otherwise read as "offline" and hide the fact that
// the agent has been archived). Per the RFC decision (see MUL-2319), we
// surface archived agents in this endpoint rather than filtering them
// out in the SQL.
func deriveSquadMemberStatus(
	archived bool,
	runtimeStatus pgtype.Text,
	lastSeen pgtype.Timestamptz,
	hasWorkingTask bool,
	now time.Time,
) string {
	if archived {
		return "archived"
	}
	if hasWorkingTask {
		return "working"
	}
	availability := deriveRuntimeAvailability(runtimeStatus, lastSeen, now)
	if availability == "online" {
		return "idle"
	}
	return availability
}

// ── Squad Leader Evaluation ──────────────────────────────────────────────────

// ── Squad Trigger Logic ─────────────────────────────────────────────────────

// shouldSuppressSquadLeaderSelfTrigger reports whether a squad leader's own
// comment should be blocked from re-enqueuing that same leader. The only
// leader-authored non-leader task allowed to wake the assigned leader is a
// same-squad worker task; generic agent tasks such as direct mentions and
// thread-parent replies are not worker-role proof and must not self-trigger.
func (h *Handler) shouldSuppressSquadLeaderSelfTrigger(ctx context.Context, issueID, leaderID, squadID pgtype.UUID) bool {
	latest, err := h.Queries.GetLatestTaskRoleForIssueAndAgent(ctx, db.GetLatestTaskRoleForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: leaderID,
	})
	if err != nil {
		return false
	}
	if latest.IsLeaderTask {
		return true
	}
	return !latest.SquadID.Valid || uuidToString(latest.SquadID) != uuidToString(squadID)
}

// commentMentionsAnyone returns true when the comment body contains at least
// one routing-style mention — [@Name](mention://agent|member|squad|all/<id>).
// Issue cross-references (mention://issue/...) are ignored because they are
// not directed at a participant. Only the current comment is inspected —
// parent (thread root) mentions are NOT inherited here.
func commentMentionsAnyone(content string) bool {
	for _, m := range util.ParseMentions(content) {
		switch m.Type {
		case "agent", "member", "squad", "all":
			return true
		}
	}
	return false
}

// The squad-leader assign/promotion readiness decision now lives in the single
// service.IssueService.WillEnqueueRun predicate (MUL-3375), shared by the issue
// write paths and the preview endpoint. The former handler-local mirrors
// (shouldEnqueueSquadLeaderOnAssign / isSquadLeaderReady) were removed to stop
// the four-entry-point drift. The squad enqueue side effect still flows through
// enqueueSquadLeaderTask below, which keeps the leader access gate and pending
// dedup in one place.

// enqueueSquadLeaderTask triggers the squad leader agent for an issue assigned
// to a squad. Assign and backlog-promotion paths use this directly; comment
// paths go through computeCommentAgentTriggers so preview and create share the
// same trigger set.
// It returns true only when a leader task was actually enqueued.
func (h *Handler) enqueueSquadLeaderTask(ctx context.Context, issue db.Issue, triggerCommentID pgtype.UUID, authorType, authorID, handoffNote string) bool {
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}

	// The gate must judge the SAME top-of-chain human the enqueue path will
	// persist on the leader task row, or it drifts: an agent-created issue that
	// correctly inherits its originator (MUL-4305) would still be denied here
	// if the gate used an empty originator. Member authors are their own
	// originator; for agent/system-triggered assigns we resolve the originator
	// exactly like EnqueueTaskForSquadLeader* does (via the issue's origin
	// link). triggerCommentID is always empty on the assign/promote path, so we
	// pass an invalid UUID to match. A still-unresolved originator leaves
	// leaderOriginator empty, which correctly fails closed for member/team
	// targets while a workspace target still admits the agent principal.
	leaderOriginator := ""
	if authorType == "member" {
		leaderOriginator = authorID
	} else {
		leaderOriginator = uuidToString(h.TaskService.OriginatorForIssueTask(ctx, issue, pgtype.UUID{}))
	}
	if !h.canEnqueueSquadLeader(ctx, squad.LeaderID, authorType, authorID, leaderOriginator, uuidToString(issue.WorkspaceID)) {
		return false
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: squad.LeaderID,
		// Key dedup on the reviewed head (TEN-356).
		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, issue.ID),
	})
	if err != nil || hasPending {
		return false
	}

	// triggerCommentID is always empty on the assign/promote path; legacy
	// handoff text rides its dedicated task column instead.
	_ = triggerCommentID
	// The member who performed the assign/promote is the accountable human for the
	// leader run (MUL-4302 §4) — the same principal the gate above judged. An agent
	// author is not a human, so only a member actor is threaded.
	if _, err := h.TaskService.EnqueueTaskForSquadLeaderWithHandoff(ctx, issue, squad.LeaderID, squad.ID, handoffNote, memberActorUserID(authorType, authorID)); err != nil {
		slog.Warn("enqueue squad leader task failed",
			"issue_id", uuidToString(issue.ID),
			"squad_id", uuidToString(squad.ID),
			"leader_id", uuidToString(squad.LeaderID),
			"error", err)
		return false
	}
	return true
}
