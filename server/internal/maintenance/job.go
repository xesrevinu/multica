// Package maintenance runs explicitly requested, bounded database maintenance.
// It has no scheduler: each call advances one transaction on the primary database.
package maintenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"database/sql"
	"strings"
	"sync"

)

var (
	ErrInvalid   = errors.New("invalid maintenance request")
	ErrConflict  = errors.New("maintenance job conflict")
	ErrBusy      = errors.New("maintenance job busy")
	ErrThrottled = errors.New("maintenance job rate limited")
	ErrNotFound  = errors.New("maintenance job not found")
)

type scanner interface{ Scan(dest ...any) error }

type Options struct {
	BatchSize          int `json:"batch_size"`
	DelayMS            int `json:"delay_ms"`
	LockTimeoutMS      int `json:"lock_timeout_ms"`
	StatementTimeoutMS int `json:"statement_timeout_ms"`
}

func (o *Options) normalize() error {
	if o.BatchSize == 0 {
		o.BatchSize = 500
	}
	if o.DelayMS == 0 {
		o.DelayMS = 100
	}
	if o.LockTimeoutMS == 0 {
		o.LockTimeoutMS = 500
	}
	if o.StatementTimeoutMS == 0 {
		o.StatementTimeoutMS = 3000
	}
	if o.BatchSize < 1 || o.BatchSize > 5000 || o.DelayMS < 1 || o.DelayMS > 60000 ||
		o.LockTimeoutMS < 1 || o.StatementTimeoutMS < o.LockTimeoutMS || o.StatementTimeoutMS > 5000 {
		return fmt.Errorf("%w: batch_size 1..5000, delay_ms 1..60000, 1 <= lock_timeout_ms <= statement_timeout_ms <= 5000", ErrInvalid)
	}
	return nil
}

type Job struct {
	ID             string          `json:"id"`
	Type           string          `json:"job_type"`
	Version        int             `json:"job_version"`
	Scope          string          `json:"scope_key"`
	IdempotencyKey string          `json:"idempotency_key"`
	RequestHash    string          `json:"-"`
	Status         string          `json:"status"`
	Revision       int64           `json:"revision"`
	DryRun         bool            `json:"dry_run"`
	Options        Options         `json:"options"`
	Parameters     json.RawMessage `json:"parameters"`
	Checkpoint     json.RawMessage `json:"checkpoint"`
	Progress       json.RawMessage `json:"progress"`
	Result         json.RawMessage `json:"result"`
	LastError      string          `json:"last_error"`
	NextAllowedAt  time.Time       `json:"next_allowed_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
}
type CreateRequest struct {
	Type           string          `json:"job_type"`
	Version        int             `json:"job_version"`
	Scope          string          `json:"scope_key"`
	IdempotencyKey string          `json:"idempotency_key"`
	DryRun         *bool           `json:"dry_run,omitempty"`
	Options        Options         `json:"options"`
	Parameters     json.RawMessage `json:"parameters"`
}

// Processor implementations must only write through the supplied transaction.
// External side effects cannot share this checkpoint's atomicity guarantee.
type Processor interface {
	Type() string
	Version() int
	Validate(scope string, dryRun bool, parameters json.RawMessage) (json.RawMessage, error)
	Preflight(context.Context, *sql.Tx) error
	Step(context.Context, *sql.Tx, Job) (checkpoint, progress, result json.RawMessage, complete bool, err error)
}
var jobLocks sync.Map

func tryLockJob(id string) (func(), bool) {
	muI, _ := jobLocks.LoadOrStore(id, &sync.Mutex{})
	mu := muI.(*sync.Mutex)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

type Service struct {
	pool       *sql.DB
	processors map[string]Processor
}

func NewService(pool *sql.DB, processors ...Processor) *Service {
	s := &Service{pool: pool, processors: make(map[string]Processor)}
	for _, p := range processors {
		if _, ok := s.processors[p.Type()]; ok {
			panic("duplicate maintenance processor")
		}
		s.processors[p.Type()] = p
	}
	return s
}
func decode(raw []byte, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return fmt.Errorf("%w: expected JSON object", ErrInvalid)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: expected one JSON object", ErrInvalid)
	}
	return nil
}
func asJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

const columns = `id, job_type, job_version, scope_key, idempotency_key, request_hash, status, revision,
dry_run, options, parameters, checkpoint, progress, result, last_error, next_allowed_at,
created_at, updated_at, completed_at`

func scan(row scanner) (Job, error) {
	var j Job
	var opts, params, cp, prog, res, nextAllowedStr, createdStr, updatedStr string
	var completedStr sql.NullString
	err := row.Scan(&j.ID, &j.Type, &j.Version, &j.Scope, &j.IdempotencyKey, &j.RequestHash, &j.Status, &j.Revision,
		&j.DryRun, &opts, &params, &cp, &prog, &res, &j.LastError,
		&nextAllowedStr, &createdStr, &updatedStr, &completedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	if err != nil {
		return j, err
	}
	j.Parameters = json.RawMessage(params)
	j.Checkpoint = json.RawMessage(cp)
	j.Progress = json.RawMessage(prog)
	j.Result = json.RawMessage(res)
	j.NextAllowedAt, _ = parseMaintenanceTime(nextAllowedStr)
	j.CreatedAt, _ = parseMaintenanceTime(createdStr)
	j.UpdatedAt, _ = parseMaintenanceTime(updatedStr)
	if completedStr.Valid && completedStr.String != "" {
		if t, err := parseMaintenanceTime(completedStr.String); err == nil {
			j.CompletedAt = &t
		}
	}
	err = decode([]byte(opts), &j.Options)
	return j, err
}
func (s *Service) processor(j Job) (Processor, error) {
	p, ok := s.processors[j.Type]
	if !ok || p.Version() != j.Version {
		return nil, fmt.Errorf("%w: unsupported job type/version", ErrConflict)
	}
	return p, nil
}
func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	return scan(s.pool.QueryRowContext(ctx, "SELECT "+columns+" FROM maintenance_job WHERE id=$1", id))
}
func setTimeouts(ctx context.Context, tx *sql.Tx, o Options) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true), set_config('statement_timeout', $2, true)`,
		fmt.Sprintf("%dms", o.LockTimeoutMS), fmt.Sprintf("%dms", o.StatementTimeoutMS))
	return err
}
func classify(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrConflict
	}
	if strings.Contains(err.Error(), "database is locked") {
		return ErrBusy
	}
	return err
}
func (s *Service) Create(ctx context.Context, r CreateRequest) (Job, error) {
	if r.IdempotencyKey == "" || len(r.IdempotencyKey) > 200 || len(r.Scope) > 200 {
		return Job{}, ErrInvalid
	}
	if err := r.Options.normalize(); err != nil {
		return Job{}, err
	}
	dry := r.DryRun == nil || *r.DryRun
	j := Job{Type: r.Type, Version: r.Version, Scope: r.Scope, DryRun: dry, Options: r.Options}
	p, err := s.processor(j)
	if err != nil {
		return Job{}, err
	}
	if len(r.Parameters) == 0 {
		r.Parameters = asJSON(struct{}{})
	}
	params, err := p.Validate(r.Scope, dry, r.Parameters)
	if err != nil {
		return Job{}, err
	}
	// Keep creation identity stable even after operational options are tuned.
	hash := fmt.Sprintf("%x", sha256.Sum256(asJSON([]any{r.Type, r.Version, r.Scope, dry, r.Options, params})))
	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	if err = setTimeouts(ctx, tx, r.Options); err != nil {
		return Job{}, err
	}
	// Every processor relies on these shared identity/concurrency constraints.
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master
 WHERE type='index' AND tbl_name='maintenance_job' AND name IN
 ('idx_maintenance_job_id','idx_maintenance_job_idempotency','idx_maintenance_job_active')`).Scan(&count); err != nil {
		return Job{}, err
	}
	if count != 3 {
		return Job{}, fmt.Errorf("maintenance indexes are missing or invalid; finish migrations 480-482")
	}
	existing, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM maintenance_job WHERE idempotency_key=$1", r.IdempotencyKey))
	if err == nil {
		if existing.RequestHash != hash {
			return existing, ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Job{}, err
	}
	if err = p.Preflight(ctx, tx); err != nil {
		return Job{}, err
	}
	j, err = scan(tx.QueryRowContext(ctx, `INSERT INTO maintenance_job
 (job_type,job_version,scope_key,idempotency_key,status,dry_run,options,parameters,request_hash)
 VALUES ($1,$2,$3,$4,'ready',$5,$6,$7,$8) RETURNING `+columns,
		r.Type, r.Version, r.Scope, r.IdempotencyKey, dry, string(asJSON(r.Options)), string(params), hash))
	if err != nil {
		return Job{}, classify(err)
	}
	return j, tx.Commit()
}

// Mutate uses a row lock and a caller-supplied revision. Stale retries return
// the durable state without advancing again, including after an ambiguous commit.
func (s *Service) Mutate(ctx context.Context, id string, revision int64, action string) (Job, error) {
	return s.mutate(ctx, id, revision, action, nil)
}

// Configure changes operational limits only while paused; task parameters and
// processor versions are immutable for a run.
func (s *Service) Configure(ctx context.Context, id string, revision int64, options Options) (Job, error) {
	if err := options.normalize(); err != nil {
		return Job{}, err
	}
	return s.mutate(ctx, id, revision, "configure", &options)
}

func (s *Service) mutate(ctx context.Context, id string, revision int64, action string, options *Options) (Job, error) {
	if revision < 0 {
		return Job{}, ErrInvalid
	}
	unlock, ok := tryLockJob(id)
	if !ok {
		return Job{}, ErrBusy
	}
	defer unlock()
	tx, err := s.pool.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	if err = setTimeouts(ctx, tx, Options{LockTimeoutMS: 500, StatementTimeoutMS: 5000}); err != nil {
		return Job{}, err
	}
	j, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM maintenance_job WHERE id=$1", id))
	if err != nil {
		return Job{}, classify(err)
	}
	if j.Revision != revision {
		return j, ErrConflict
	}
	if j.Status == "completed" || j.Status == "cancelled" {
		return j, ErrConflict
	}
	var stepErr error
	switch action {
	case "configure":
		if j.Status != "paused" || options == nil {
			return j, ErrConflict
		}
		j.Options = *options
	case "pause":
		j.Status = "paused"
	case "cancel":
		j.Status = "cancelled"
	case "resume":
		j.Status = "ready"
	case "advance":
		if j.Status != "ready" {
			return j, ErrConflict
		}
		p, e := s.processor(j)
		if e != nil {
			return j, e
		}
		if time.Now().UTC().Before(j.NextAllowedAt) {
			return j, ErrThrottled
		}
		if _, e = p.Validate(j.Scope, j.DryRun, j.Parameters); e != nil {
			return j, e
		}
		if e = j.Options.normalize(); e != nil {
			return j, e
		}
		// Savepoint lets failures retain diagnostics without retaining partial data.
		if _, e = tx.ExecContext(ctx, "SAVEPOINT maintenance_batch"); e != nil {
			return j, e
		}
		batch := tx
		e = setTimeouts(ctx, batch, j.Options)
		if e == nil {
			e = p.Preflight(ctx, batch)
		}
		var cp, progress, result json.RawMessage
		var complete bool
		if e == nil {
			cp, progress, result, complete, e = p.Step(ctx, batch, j)
		}
		if e != nil {
			if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT maintenance_batch"); rollbackErr != nil {
				return j, e
			}
			_, _ = tx.ExecContext(ctx, "RELEASE SAVEPOINT maintenance_batch")
			stepErr = e
			j.LastError = e.Error()
			// Repeated failures require an explicit operator resume.
			j.Status = "paused"
		} else {
			if _, e = tx.ExecContext(ctx, "RELEASE SAVEPOINT maintenance_batch"); e != nil {
				return j, e
			}
			j.Checkpoint, j.Progress, j.Result = cp, progress, result
			j.LastError = ""
			if complete {
				j.Status = "completed"
			}
		}
	default:
		return j, ErrInvalid
	}
	nowUTC := time.Now().UTC()
	nowStr := formatMaintenanceTime(nowUTC)
	nextAllowed := j.NextAllowedAt
	if action == "advance" || action == "configure" {
		cand := nowUTC.Add(time.Duration(j.Options.DelayMS) * time.Millisecond)
		if cand.After(nextAllowed) {
			nextAllowed = cand
		}
	}
	var completedAt sql.NullString
	if j.Status == "completed" || j.Status == "cancelled" {
		completedAt = sql.NullString{String: nowStr, Valid: true}
	} else if j.CompletedAt != nil {
		completedAt = sql.NullString{String: formatMaintenanceTime(*j.CompletedAt), Valid: true}
	}
	j, err = scan(tx.QueryRowContext(ctx, `UPDATE maintenance_job SET status=$2, revision=revision+1,
 checkpoint=$3,progress=$4,result=$5,last_error=$6,updated_at=$7,
	next_allowed_at=$8,
	options=$9,
 completed_at=$10
 WHERE id=$1 RETURNING `+columns, id, j.Status, string(j.Checkpoint), string(j.Progress), string(j.Result), j.LastError,
		nowStr, formatMaintenanceTime(nextAllowed), string(asJSON(j.Options)), completedAt))
	if err != nil {
		return j, err
	}
	if err = tx.Commit(); err != nil {
		return Job{}, err
	}
	slog.Info("maintenance job operation committed", "job_id", j.ID, "job_type", j.Type, "action", action, "status", j.Status, "revision", j.Revision, "progress", string(j.Progress))
	return j, stepErr
}

func formatMaintenanceTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseMaintenanceTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}
