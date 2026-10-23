package lark

import (
	"context"
	"database/sql"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (f *fakeTypingQueries) RegisterChannelTypingReaction(_ context.Context, p db.RegisterChannelTypingReactionParams) (db.ChannelTypingReaction, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	if f.ledger == nil {
		f.ledger = make(map[string]db.ChannelTypingReaction)
	}
	r := db.ChannelTypingReaction{ID: p.ID, WorkspaceID: p.WorkspaceID, ChatSessionID: p.ChatSessionID, ChatMessageID: p.ChatMessageID, InstallationID: p.InstallationID, ChannelMessageID: p.ChannelMessageID, InstallationSnapshot: p.InstallationSnapshot}
	f.ledger[p.ID] = r
	return r, nil
}
func (f *fakeTypingQueries) FinishChannelTypingReactionAdd(_ context.Context, p db.FinishChannelTypingReactionAddParams) (db.ChannelTypingReaction, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	r, ok := f.ledger[p.ID]
	if !ok {
		return r, sql.ErrNoRows
	}
	r.ReactionID = p.ReactionID
	r.AddFinished = 1
	if p.CleanupRequired != 0 {
		r.CleanupRequired = 1
	}
	r.CleanedAt = sql.NullString{}
	f.ledger[p.ID] = r
	return r, nil
}
func (f *fakeTypingQueries) AcknowledgeChannelTypingReactionCleanup(_ context.Context, p db.AcknowledgeChannelTypingReactionCleanupParams) error {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	r := f.ledger[p.ID]
	if r.AddFinished != 0 && r.ReactionID != "" && r.ReactionID == p.ReactionID {
		r.CleanedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
		f.ledger[p.ID] = r
	}
	return nil
}

// Existing event unit fixtures supply terminal events directly rather than
// mutating DB tasks. Real eligibility is covered by the DB regression matrix.
func (f *fakeTypingQueries) ClaimChannelTypingReactionCleanup(_ context.Context, session any) ([]db.ChannelTypingReaction, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	sessStr, _ := session.(string)
	var rows []db.ChannelTypingReaction
	for id, r := range f.ledger {
		if !r.CleanedAt.Valid && (sessStr == "" || r.ChatSessionID == sessStr) {
			r.CleanupRequired = 1
			f.ledger[id] = r
			rows = append(rows, r)
		}
	}
	return rows, nil
}
func (f *fakeTypingQueries) SettleChannelTypingInputs(context.Context, db.SettleChannelTypingInputsParams) error {
	return nil
}
func (f *fakeTypingQueries) SkipChannelTypingReactionAdd(context.Context, string) error {
	return nil
}
func (f *fakeTypingQueries) PruneChannelTypingReactionCleanup(context.Context) error { return nil }

func (f *fakeTypingQueries) ListRequestedChannelTypingReactions(_ context.Context, idsAny any) ([]string, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	var ids []string
	switch v := idsAny.(type) {
	case []string:
		ids = v
	}
	var closed []string
	for _, id := range ids {
		if f.ledger[id].CleanupRequired != 0 {
			closed = append(closed, id)
		}
	}
	return closed, nil
}

func (f *fakeTypingQueries) ExpireChannelTypingReactionCleanup(context.Context) ([]db.ExpireChannelTypingReactionCleanupRow, error) {
	return nil, nil
}
