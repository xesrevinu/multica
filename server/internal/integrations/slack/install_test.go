package slack

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func testBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func mustUUID(t *testing.T, s string) string {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

type fakeInstallQueries struct {
	existing         *db.ChannelInstallation
	appIDTaken       bool
	upsertParams     db.UpsertChannelInstallationParams
	upsertCalled     bool
	rowID            string
	reclaimedID      *string
	reclaimCalled    bool
	ownerWorkspaceID string
	ownerArchived    bool
	ownerMissing     bool
}

func (f *fakeInstallQueries) WithTx(_ *sql.Tx) installQueries { return f }

func (f *fakeInstallQueries) ReclaimDeadChannelInstallationByAppID(_ context.Context, _ db.ReclaimDeadChannelInstallationByAppIDParams) (string, error) {
	f.reclaimCalled = true
	if f.reclaimedID != nil {
		return *f.reclaimedID, nil
	}
	return "", sql.ErrNoRows
}

func (f *fakeInstallQueries) GetChannelInstallationOwnerByAppID(_ context.Context, _ db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error) {
	if f.ownerMissing {
		return db.GetChannelInstallationOwnerByAppIDRow{}, sql.ErrNoRows
	}
	var archived sql.NullString
	if f.ownerArchived {
		archived = sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
	}
	return db.GetChannelInstallationOwnerByAppIDRow{
		WorkspaceID:     f.ownerWorkspaceID,
		AgentArchivedAt: archived,
	}, nil
}

func (f *fakeInstallQueries) UpsertChannelInstallation(_ context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error) {
	f.upsertCalled = true
	f.upsertParams = arg
	if f.appIDTaken {
		return db.ChannelInstallation{}, errors.New("UNIQUE constraint failed: channel_installation")
	}
	id := f.rowID
	if f.existing != nil {
		id = f.existing.ID
	}
	return db.ChannelInstallation{
		ID:              id,
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     arg.ChannelType,
		Config:          arg.Config,
		InstallerUserID: arg.InstallerUserID,
		Status:          "active",
	}, nil
}

func (f *fakeInstallQueries) ListChannelInstallationsByWorkspace(_ context.Context, _ db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error) {
	return nil, nil
}

func (f *fakeInstallQueries) GetChannelInstallationInWorkspace(_ context.Context, _ db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, nil
}

func (f *fakeInstallQueries) SetChannelInstallationStatus(_ context.Context, _ db.SetChannelInstallationStatusParams) error {
	return nil
}

var (
	slackTxDBOnce sync.Once
	slackTxDB     *sql.DB
)

type fakeTxStarter struct{}

func (f *fakeTxStarter) Begin(ctx context.Context) (*sql.Tx, error) {
	slackTxDBOnce.Do(func() {
		var err error
		slackTxDB, err = testutil.OpenTestPool()
		if err != nil {
			panic(err)
		}
	})
	return slackTxDB.BeginTx(ctx, nil)
}

func newTestInstallService(t *testing.T, q installQueries) *InstallService {
	t.Helper()
	svc, err := newInstallService(q, &fakeTxStarter{}, testBox(t), nil)
	if err != nil {
		t.Fatalf("newInstallService: %v", err)
	}
	return svc
}
