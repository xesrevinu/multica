package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/dbreader"
	"github.com/multica-ai/multica/server/internal/dbstartup"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/maintenance"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/profiling"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/scheduler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/tracing"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/llm"
)

var (
	version = "dev"
	commit  = "unknown"
)

func envPositiveInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func envNonNegativeInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

// maxLLMRetriesLimit caps MULTICA_LLM_MAX_RETRIES. The ceiling is a latency
// budget, not a taste call: SDK backoff is 0.5s doubling to an 8s cap, so 6
// retries spend ~21s and 10 spend ~48s sleeping before the last attempt. Every
// internal caller of pkg/llm runs under a far tighter deadline (8s for chat
// quick actions, 20s for title generation), so a budget past 5 cannot finish —
// it only converts a retryable upstream failure into a deadline-exceeded one.
const maxLLMRetriesLimit = 5

// parseLLMMaxRetries turns the raw MULTICA_LLM_MAX_RETRIES value into the
// tri-state llm.Config.MaxRetries expects: nil for unset (use the default),
// llm.Retries(0) to disable retries, llm.Retries(N) for a ceiling of N.
//
// Unlike the envFooInt helpers above it returns an error instead of warning and
// falling back to a default. A retry budget silently corrected to something the
// operator did not ask for is the failure this knob exists to remove
// (MUL-6364): a typo'd "3x" or a negative must stop the boot, not quietly
// restore the default and look configured.
func parseLLMMaxRetries(raw string) (*llm.RetryOverride, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("must be an integer, got %q", raw)
	}
	if v > maxLLMRetriesLimit {
		return nil, fmt.Errorf("must be at most %d, got %d", maxLLMRetriesLimit, v)
	}
	// llm.Retries owns the lower bound. It is the boundary that makes a negative
	// budget unrepresentable, and this is the only place the server builds one,
	// so the deployment-specific ceiling above and the type-level floor here
	// cannot disagree.
	override, err := llm.Retries(v)
	if err != nil {
		return nil, fmt.Errorf("must not be negative, got %d (use 0 to disable retries)", v)
	}
	return override, nil
}

// parseLLMDisableThinking turns the raw MULTICA_LLM_DISABLE_THINKING value
// into the bool llm.Config.DisableThinking expects. It follows the
// parseLLMMaxRetries contract: unset is a valid state (the knob stays off),
// and anything that does not read as a deliberate true/false stops the boot
// instead of being coerced — a typo'd "ture" that silently did nothing would
// leave an operator debugging latency the configuration claims to remove.
func parseLLMDisableThinking(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return false, nil
	case "1", "true":
		return true, nil
	case "0", "false":
		return false, nil
	default:
		return false, fmt.Errorf("must be a boolean (true/false or 1/0), got %q", raw)
	}
}

func envDuration(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func envNonNegativeDuration(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func holdBeforeShutdown(sig os.Signal, signals <-chan os.Signal, duration time.Duration) {
	if duration <= 0 {
		return
	}
	slog.Info("termination signal received; holding before shutdown",
		"signal", sig.String(),
		"duration", duration.String(),
	)
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		slog.Info("shutdown hold complete", "duration", duration.String())
	case interruptSig := <-signals:
		slog.Info("shutdown hold interrupted by signal",
			"signal", interruptSig.String(),
			"configured_duration", duration.String(),
		)
	}
}

func envBool(name string, def bool) bool {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func backgroundServices(h *handler.Handler) (*service.TaskService, *service.AutopilotService) {
	return h.TaskService, h.AutopilotService
}

// jwtSecretBootError returns a non-nil error when the combination of
// JWT_SECRET and APP_ENV is unsafe to boot with: production must never run
// on an empty or publicly-known default secret (auth.ValidateJWTSecret).
// Non-production keeps the historical dev fallback (see auth.JWTSecret)
// and only warns.
func jwtSecretBootError(jwtSecret, appEnv string) error {
	isProduction := strings.EqualFold(strings.TrimSpace(appEnv), "production")
	if !isProduction {
		return nil
	}
	return auth.ValidateJWTSecret(jwtSecret)
}

// newMainHTTPServer builds the public HTTP server with the production timeout
// defaults. These values are load-bearing safety settings, not cosmetic tuning,
// so they live in one helper that main() and the config regression test share.
//
// Bound header reads to stop Slowloris; IdleTimeout is looser than the
// metrics/profiling servers' 30s for keep-alive-heavy CLI and daemon clients.
// ReadTimeout and WriteTimeout are deliberately left zero so WebSocket upgrades
// (/ws, /api/daemon/ws) on this listener aren't killed mid-connection.
func newMainHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func main() {
	logger.Init()
	tracingShutdown, err := tracing.Setup(context.Background(), tracing.ConfigFromEnv(), version)
	if err != nil {
		slog.Error("tracing setup failed", "error", err)
		os.Exit(1)
	}
	// Warn about missing configuration
	if err := jwtSecretBootError(os.Getenv("JWT_SECRET"), os.Getenv("APP_ENV")); err != nil {
		slog.Error(
			"refusing to start: "+err.Error()+
				"; generate a strong secret with `openssl rand -hex 32` and set JWT_SECRET (see .env.example)",
			"app_env", os.Getenv("APP_ENV"),
		)
		os.Exit(1)
	}
	if os.Getenv("JWT_SECRET") == "" {
		slog.Warn("JWT_SECRET is not set — using insecure dev default (allowed only because APP_ENV is not production).")
	}
	if os.Getenv("RESEND_API_KEY") == "" && strings.TrimSpace(os.Getenv("SMTP_HOST")) == "" {
		slog.Warn("no email backend configured (RESEND_API_KEY and SMTP_HOST both empty) — verification codes will be printed to the log instead of emailed.")
	}
	if os.Getenv("MULTICA_DEV_VERIFICATION_CODE") != "" {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
			slog.Warn("MULTICA_DEV_VERIFICATION_CODE is set but ignored because APP_ENV=production.")
		} else {
			slog.Warn("MULTICA_DEV_VERIFICATION_CODE is enabled. Use it only for local development or private test instances.")
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	shutdownHoldDuration := envNonNegativeDuration("MULTICA_SHUTDOWN_HOLD_DURATION", 0)

	// Feature flags: loaded once at startup from MULTICA_FEATURE_FLAGS_FILE
	// (a YAML rule set) with FF_<KEY> env overrides layered on top.
	// See server/pkg/featureflag for the schema and lifecycle rules.
	//
	// Booting the server without any flag config is intentional: when the
	// env var is unset, every IsEnabled call falls through to the caller's
	// default, so existing code paths are unchanged until someone adds a
	// rule. A misconfigured (malformed / missing) file surfaces as a hard
	// error so operators see misconfig the same way they do for any other
	// MULTICA_*_FILE knob.
	flags, err := featureflag.NewServiceFromEnv(featureflag.WithLogger(slog.Default()))
	if err != nil {
		slog.Error("feature flag configuration failed to load", "error", err)
		os.Exit(1)
	}
	_ = flags // adopted by the router (opts.FeatureFlags) and server-side toggle points; see server/pkg/featureflag

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "file:./data/multica.db"
	}

	startupSettings := dbstartup.SettingsFromEnv()
	pool, err := newDBPool(context.Background(), dbURL, startupSettings.ConnectTimeout)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	startupCtx, stopStartup := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	retryOptions := startupSettings.RetryOptions()
	retryOptions.ShouldRetry = dbstartup.IsTransientDatabaseError
	retryOptions.OnRetry = func(event dbstartup.RetryEvent) {
		slog.Warn("database unavailable during server startup; retrying",
			"attempt", event.Attempt,
			"retry_in", event.Delay,
			"error", event.Err,
		)
	}
	if err := dbstartup.Retry(startupCtx, retryOptions, pool.PingContext); err != nil {
		stopStartup()
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}
	stopStartup()
	slog.Info("connected to database")
	logPoolConfig("primary", pool)

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	daemonHub := daemonws.NewHub()
	var daemonWakeup interface {
		service.TaskWakeupNotifier
		handler.RuntimeGoneNotifier
	} = daemonHub
	// The in-memory hub is the only broadcaster. There is no cross-node relay.
	var broadcaster realtime.Broadcaster = hub
	stopRelay := func() {}
	slog.Info("realtime: using in-memory hub (single-node mode)")
	registerListeners(bus, broadcaster)

	analyticsClient := analytics.NewFromEnv()
	defer analyticsClient.Close()

	queries := db.New(pool)
	hub.SetAuthorizer(newScopeAuthorizer(queries))
	// Order matters: subscriber listeners must register BEFORE notification listeners.
	// The notification listener queries the subscriber table to determine recipients,
	// so subscribers must be written first within the same synchronous event dispatch.
	registerSubscriberListeners(bus, pool)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)

	metricsConfig := obsmetrics.ConfigFromEnv()
	var metricsServer *http.Server
	var httpMetrics *obsmetrics.HTTPMetrics
	var businessMetrics *obsmetrics.BusinessMetrics
	var channelMediaMetrics *obsmetrics.ChannelMediaReconcilerMetrics
	var channelLeaseMetrics *obsmetrics.ChannelLeaseMetrics
	var dbRoutingMetrics *obsmetrics.DBRoutingMetrics
	if metricsConfig.Enabled() {
		metricsRegistry := obsmetrics.NewRegistry(obsmetrics.RegistryOptions{
			Pool:     pool,
			Realtime: realtime.M,
			DaemonWS: daemonws.M,
			Version:  version,
			Commit:   commit,
		})
		httpMetrics = metricsRegistry.HTTP
		businessMetrics = metricsRegistry.Business
		channelMediaMetrics = metricsRegistry.ChannelMedia
		channelLeaseMetrics = metricsRegistry.ChannelLease
		dbRoutingMetrics = metricsRegistry.DBRouting
		// Forward inbound daemon WS frames into the per-kind counter so
		// dashboards can split heartbeat / unknown / invalid traffic.
		if daemonHub != nil {
			daemonHub.SetMessageKindRecorder(businessMetrics)
		}
		metricsServer = obsmetrics.NewServer(metricsConfig.Addr, metricsRegistry.Gatherer)
		if !obsmetrics.IsLoopbackAddr(metricsConfig.Addr) {
			slog.Warn(
				"metrics listener is not loopback-only; restrict access with private networking, allowlists, or proxy auth",
				"addr", metricsConfig.Addr,
			)
		}
	}
	// Construct the BatchedHeartbeatScheduler before the router so it can
	// be injected into the Handler. The Run goroutine starts below
	// alongside the sweeper, and Stop is called explicitly during graceful
	// shutdown so any pending bumps are flushed before we exit.
	heartbeatScheduler := handler.NewBatchedHeartbeatScheduler(queries, handler.DefaultHeartbeatBatchInterval, daemonWakeup)

	// Validate the LLM retry budget before the router exists: an operator who
	// typed a value we cannot honor should see the boot stop, the same way a
	// malformed feature-flag file does above.
	llmMaxRetries, err := parseLLMMaxRetries(os.Getenv("MULTICA_LLM_MAX_RETRIES"))
	if err != nil {
		slog.Error("invalid MULTICA_LLM_MAX_RETRIES", "error", err)
		os.Exit(1)
	}
	var readRecorder dbreader.Recorder
	if dbRoutingMetrics != nil {
		readRecorder = dbRoutingMetrics
	}

	// Same contract for the thinking-off hint: an unparseable value must stop
	// the boot rather than read as "disabled" and look configured.
	llmDisableThinking, err := parseLLMDisableThinking(os.Getenv("MULTICA_LLM_DISABLE_THINKING"))
	if err != nil {
		slog.Error("invalid MULTICA_LLM_DISABLE_THINKING", "error", err)
		os.Exit(1)
	}

	r, h := NewRouterWithOptions(pool, hub, bus, analyticsClient, RouterOptions{
		HTTPMetrics:         httpMetrics,
		BusinessMetrics:     businessMetrics,
		ChannelLeaseMetrics: channelLeaseMetrics,
		DaemonHub:           daemonHub,
		DaemonWakeup:        daemonWakeup,
		FeatureFlags:        flags,
		HeartbeatScheduler:  heartbeatScheduler,
		LLMMaxRetries:       llmMaxRetries,
		LLMDisableThinking:  llmDisableThinking,
	})
	// SQLite is a single local database. There is no replica pool.
	h.ReadSelector = dbreader.New(h.Queries, nil, readRecorder)
	h.PRRefresh.SetReadSelector(h.ReadSelector)
	// In-app orchestrator: consumes the bus, reconciles every minute and
	// applies decisions through the router as its service member.
	if h.Orchestrator != nil {
		h.Orchestrator.Start(context.Background(), r)
	}

	// Reconciled race recoveries in the batched scheduler reuse the same
	// daemon:register refresh the sync transition path publishes. Wired before
	// the scheduler's Run goroutine starts so the field write is race-free.
	heartbeatScheduler.RecoveryNotifier = h

	srv := newMainHTTPServer(":"+port, r)
	profilingServer := profiling.NewServer()
	maintenanceServer, maintenanceErr := maintenance.NewServer(os.Getenv("MAINTENANCE_PORT"), maintenance.NewService(pool, maintenance.StatusCategory{}))
	if maintenanceErr != nil {
		slog.Error("maintenance listener disabled", "error", maintenanceErr)
	}

	// Start background workers.
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	autopilotCtx, autopilotCancel := context.WithCancel(context.Background())
	// Reuse the router's services here. In particular, the router wires the
	// EmptyClaim cache into TaskService; constructing a second TaskService for
	// scheduled Autopilot dispatch would send the daemon wakeup without bumping
	// that cache's version, so an idle runtime could keep returning an empty
	// claim until the cache TTL expires.
	taskSvc, autopilotSvc := backgroundServices(h)
	registerAutopilotListeners(bus, autopilotSvc)

	// Heartbeats and the sweeper share the noop liveness store. Online
	// detection stays on the database heartbeat columns.
	var liveness handler.LivenessStore = handler.NewNoopLivenessStore()

	// Start background sweeper to mark stale runtimes as offline.
	runtimeReconnectGrace := envDuration("MULTICA_RUNTIME_RECONNECT_GRACE", defaultRuntimeReconnectGrace)
	if runtimeReconnectGrace < minimumRuntimeReconnectGrace {
		slog.Warn("runtime reconnect grace is shorter than heartbeat freshness; clamping",
			"configured", runtimeReconnectGrace,
			"minimum", minimumRuntimeReconnectGrace,
		)
		runtimeReconnectGrace = minimumRuntimeReconnectGrace
	}
	// Queued work now expires on the same runtime-liveness signal as in-flight
	// work, so there is no separate queue TTL to tune: a busy runtime keeps its
	// backlog, and a departed one retires everything it owned at once.
	go runRuntimeSweeper(sweepCtx, queries, liveness, taskSvc, bus, runtimeReconnectGrace)
	go runDelegatedFailureRecoverySweeper(sweepCtx, taskSvc)
	// Seven-day runtime retention does not share the 30-second liveness tick:
	// its bounded transactions run independently once per hour, so a slow GC
	// round cannot delay offline detection or task recovery.
	go runRuntimeGCSweeper(sweepCtx, pool, queries, taskSvc.Metrics, h)
	// Source-context cleanup is object-store work, so it gets its own goroutine
	// instead of a slot in the runtime sweep tick.
	go runSourceContextSweeper(sweepCtx, taskSvc)
	go heartbeatScheduler.Run(sweepCtx)
	go runAutopilotFailureMonitor(autopilotCtx, queries, bus, envFailureMonitorConfig())
	if autopilotSvc.QuotaEnabled() {
		go runAutopilotQuotaReconciler(autopilotCtx, autopilotSvc)
	}
	go runDBStatsLogger(sweepCtx, "primary", pool)
	if h.WebhookDeliveryWorker != nil {
		go h.WebhookDeliveryWorker.Run(sweepCtx)
	}
	if h.SeatCapacityWorker != nil {
		go h.SeatCapacityWorker.Run(sweepCtx)
	}
	if h.TelegramOutbound != nil {
		h.TelegramOutbound.Start(sweepCtx)
	}
	// GitHub PR-card API snapshot pipeline (MUL-5265): worker pool + TTL sweeper.
	// No-op when unconfigured (no App private key).
	h.PRRefresh.Start(sweepCtx)

	// Channel inbound supervisor (MUL-3620): holds the §4.4 WS lease per
	// installation and drives each channel.Channel. It is channel-agnostic,
	// not Lark-specific, but remains nil when lease startup validation fails
	// (notably Redis fail-closed readiness). With no platform registered or no
	// installation rows it simply idles. Lifecycle is bound to sweepCtx so it winds down
	// alongside the other long-running workers, AFTER the HTTP server has
	// drained.
	if h.ChannelSupervisor != nil {
		go h.ChannelSupervisor.Run(sweepCtx)
	}

	if h.LarkTyping != nil {
		go h.LarkTyping.Run(sweepCtx)
	}

	// Media intent-ledger reconciler (PR #5580): settles uploaded-but-unbound
	// channel media objects. An independent worker so object-storage latency
	// spikes cannot starve any other sweeper's cadence.
	if h.ChannelMediaReconciler != nil {
		h.ChannelMediaReconciler.Metrics = channelMediaMetrics
		go h.ChannelMediaReconciler.Run(sweepCtx)
	}

	// MUL-2957: DB-backed execution scheduler. The scheduler turns the
	// `sys_cron_executions` table into the distributed lease + audit
	// log for internal periodic jobs. The first job is
	// `rollup_task_usage_hourly`, which replaces the previously
	// operator-registered `pg_cron` entry (still safe to run
	// concurrently — the SQL function holds advisory lock 4246).
	//
	// A failure to register the job is treated as fatal here only at
	// the registration step (a duplicate name is the only realistic
	// cause and indicates a code bug). Once running, the manager
	// surfaces transient errors — DB unreachable, sys_cron_executions
	// missing because of an unusual partial-migration state — by
	// logging them on the tick that fails and retrying on the next
	// cycle, so a temporary outage does not crash the server.
	schedulerMgr := scheduler.NewManager(pool, scheduler.Options{})
	if err := schedulerMgr.Register(scheduler.TaskUsageHourlyJob(pool)); err != nil {
		slog.Warn("scheduler: failed to register task_usage_hourly rollup job", "error", err)
	}
	// MUL-3551: scheduled-Autopilot dispatch runs on the same DB-backed
	// scheduler. The job owns its plan_times via PlansForScope (each
	// trigger has its own cron expression, so the Cadence planner does
	// not fit). Crash recovery, occurrence-level idempotency, lease
	// theft, and retry are all reused from the manager + sys_cron_executions
	// — there is no separate goroutine for scheduled Autopilot anymore.
	if err := schedulerMgr.Register(scheduler.SearchIndexChangePruneJob(queries)); err != nil {
		slog.Warn("scheduler: failed to register search index change prune job", "error", err)
	}
	if err := schedulerMgr.Register(scheduler.IssueWakeupJob(&service.IssueWakeupService{Tasks: taskSvc})); err != nil {
		slog.Error("scheduler: register issue wakeups", "error", err)
	}
	if err := schedulerMgr.Register(scheduler.ChildEventSweepJob(&service.IssueWakeupService{Tasks: taskSvc})); err != nil {
		slog.Error("scheduler: register child-done sweep", "error", err)
	}
	if err := schedulerMgr.Register(scheduler.AutopilotScheduleDispatchJob(pool, queries, autopilotSvc)); err != nil {
		slog.Warn("scheduler: failed to register autopilot_schedule_dispatch job", "error", err)
	}
	// Manifest-declared Plugin schedules share the same durable lease and retry
	// machinery. The job is inert while plugins_v1 is disabled.
	if err := schedulerMgr.Register(scheduler.PluginHookScheduleDispatchJob(queries, h.PluginService)); err != nil {
		slog.Warn("scheduler: failed to register plugin_hook_schedule_dispatch job", "error", err)
	}
	go func() {
		_ = schedulerMgr.Run(sweepCtx)
	}()

	if metricsServer != nil {
		go func() {
			slog.Info("metrics server starting", "addr", metricsConfig.Addr)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server disabled after startup error", "error", err)
			}
		}()
	}

	if maintenanceServer != nil {
		go func() {
			slog.Info("maintenance server starting", "addr", maintenanceServer.Addr)
			if err := maintenanceServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("maintenance listener disabled", "error", err)
			}
		}()
	}

	go func() {
		slog.Info("pprof server starting", "addr", profilingServer.Addr)
		if err := profilingServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("pprof server disabled after startup error", "error", err)
		}
	}()

	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	holdBeforeShutdown(sig, quit, shutdownHoldDuration)
	// Restore the default behavior so another signal during graceful shutdown
	// can still terminate the process instead of being left unread in quit.
	signal.Stop(quit)

	slog.Info("shutting down server")

	// The order below is the contract, and shutdown.go is where it is stated
	// and pinned. Stop the outbound relay before cancelling workers: the
	// relay drain still needs the supervised sockets, and cancelling the
	// sweeper context is what starts tearing those sockets down.
	shutdownSequence{
		StopAutopilot: autopilotCancel,
		DrainMaintenance: func() {
			if maintenanceServer == nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
			defer cancel()
			if err := maintenanceServer.Shutdown(ctx); err != nil {
				slog.Error("maintenance shutdown interrupted", "error", err)
				_ = maintenanceServer.Close()
			}
		},
		DrainHTTP: func() {
			apiShutdownCtx, apiShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := srv.Shutdown(apiShutdownCtx); err != nil {
				apiShutdownCancel()
				slog.Error("server forced to shutdown", "error", err)
				os.Exit(1)
			}
			apiShutdownCancel()
		},
		StopOutboundRelay: stopRelay,
		CancelWorkers:     sweepCancel,
		StopHeartbeats:    heartbeatScheduler.Stop,
		JoinWebhookWorker: func() {
			if h.WebhookDeliveryWorker != nil && !h.WebhookDeliveryWorker.WaitWithTimeout(5*time.Second) {
				slog.Warn("webhook delivery worker did not exit within shutdown timeout")
			}
		},
		JoinTelegram: func() {
			if h.TelegramOutbound != nil && !h.TelegramOutbound.WaitWithTimeout(5*time.Second) {
				slog.Warn("telegram outbound workers did not exit within shutdown timeout")
			}
		},
		// Joined so the lease renewer can issue a final release before exit;
		// otherwise the next replica waits out the whole LeaseTTL on the far
		// side of a redeploy. Bounded: a wedged supervisor falls back to the
		// natural expiry rather than holding shutdown open.
		JoinChannelSupervisor: func() {
			if h.ChannelSupervisor == nil {
				return
			}
			if !h.ChannelSupervisor.WaitWithTimeout(h.ChannelSupervisor.ShutdownTimeout()) {
				slog.Warn("channel supervisor: connections did not exit within shutdown timeout; proceeding",
					"timeout", h.ChannelSupervisor.ShutdownTimeout().String(),
				)
			}
		},
		DrainChannelRouter: func() {
			if h.ChannelSupervisor == nil || h.ChannelRouter == nil {
				return
			}
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if !h.ChannelRouter.Drain(drainCtx) {
				slog.Warn("channel router: drain deadline reached; deferred media fallback remains durable")
			}
			drainCancel()
		},
		StopMetricsServer: func() {
			if metricsServer == nil {
				return
			}
			metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := metricsServer.Shutdown(metricsShutdownCtx); err != nil {
				slog.Error("metrics server forced to shutdown", "error", err)
			}
			metricsShutdownCancel()
		},
		StopProfiling: func() {
			profilingShutdownCtx, profilingShutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := profilingServer.Shutdown(profilingShutdownCtx); err != nil {
				slog.Error("pprof server forced to shutdown", "error", err)
			}
			profilingShutdownCancel()
		},
		FlushTracing: func() {
			flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := tracingShutdown(flushCtx); err != nil {
				slog.Error("tracing shutdown failed", "error", err)
			}
			flushCancel()
		},
	}.run()
	slog.Info("server stopped")
}
