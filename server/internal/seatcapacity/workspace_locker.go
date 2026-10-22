package seatcapacity

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultWorkspaceCooldown = 75 * time.Millisecond
const maxWorkspaceLockConnections = 4
const workspaceLockPoolFraction = 4

type WorkspaceLocker interface {
	Lock(context.Context, uuid.UUID) (db.DBTX, func(), error)
}

// sqliteWorkspaceLocker serializes Cloud capacity calls for one workspace in
// this process. SQLite has no session-scoped advisory locks; the database file
// lock plus this in-process gate is the serialization boundary.
type sqliteWorkspaceLocker struct {
	db *sql.DB

	mu         sync.Mutex
	localLocks map[uuid.UUID]*localWorkspaceLock
	slots      chan struct{}
}

type localWorkspaceLock struct {
	gate chan struct{}
	refs int
}

func NewWorkspaceLocker(db *sql.DB) WorkspaceLocker {
	if db == nil {
		return nil
	}
	maxOpen := db.Stats().MaxOpenConnections
	slotCount := maxOpen / workspaceLockPoolFraction
	if slotCount < 1 {
		slotCount = 1
	}
	if slotCount > maxWorkspaceLockConnections {
		slotCount = maxWorkspaceLockConnections
	}
	return &sqliteWorkspaceLocker{
		db:         db,
		localLocks: make(map[uuid.UUID]*localWorkspaceLock),
		slots:      make(chan struct{}, slotCount),
	}
}

func (l *sqliteWorkspaceLocker) Lock(ctx context.Context, workspaceID uuid.UUID) (db.DBTX, func(), error) {
	releaseLocal, err := l.acquireLocal(ctx, workspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("queue workspace capacity lock: %w", err)
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		releaseLocal()
		return nil, nil, fmt.Errorf("queue workspace capacity lock connection: %w", ctx.Err())
	}
	var once sync.Once
	return l.db, func() {
		once.Do(func() {
			defer releaseLocal()
			defer func() { <-l.slots }()
			timer := time.NewTimer(defaultWorkspaceCooldown)
			<-timer.C
		})
	}, nil
}

// acquireLocal makes same-process callers wait in Go instead of each issuing
// a capacity call while another request owns the workspace.
func (l *sqliteWorkspaceLocker) acquireLocal(ctx context.Context, workspaceID uuid.UUID) (func(), error) {
	l.mu.Lock()
	local := l.localLocks[workspaceID]
	if local == nil {
		local = &localWorkspaceLock{gate: make(chan struct{}, 1)}
		local.gate <- struct{}{}
		l.localLocks[workspaceID] = local
	}
	local.refs++
	l.mu.Unlock()

	select {
	case <-ctx.Done():
		l.dropLocalRef(workspaceID, local)
		return nil, ctx.Err()
	case <-local.gate:
		if err := ctx.Err(); err != nil {
			local.gate <- struct{}{}
			l.dropLocalRef(workspaceID, local)
			return nil, err
		}
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			local.gate <- struct{}{}
			l.dropLocalRef(workspaceID, local)
		})
	}, nil
}

func (l *sqliteWorkspaceLocker) dropLocalRef(workspaceID uuid.UUID, local *localWorkspaceLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	local.refs--
	if local.refs == 0 && l.localLocks[workspaceID] == local {
		delete(l.localLocks, workspaceID)
	}
}
