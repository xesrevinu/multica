package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/sqliteutil"
)

const (
	defaultSQLiteDatabaseURL = "file:./data/multica.db"
	defaultSQLiteSchemaPath  = "schema/sqlite.sql"
)

func maybeRunMulticall() {
	prog := filepath.Base(os.Args[0])
	switch prog {
	case "entrypoint", "multica-entrypoint":
		os.Exit(runEntrypoint(os.Args[1:]))
	case "migrate":
		os.Exit(runMigrateSubcommand(os.Args[1:]))
	}
}

func ensureSQLiteParentDir(databaseURL string) error {
	path, err := sqliteutil.FilePath(databaseURL)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite directory %q: %w", dir, err)
	}
	return nil
}

func runMigrateSubcommand(args []string) int {
	logger.Init()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) != 1 || args[0] != "up" {
		slog.Error("migration failed", "error", fmt.Sprintf("usage: migrate up (SQLite only; down is not supported). Default database is %s; override with DATABASE_URL. Schema defaults to %s; override with MULTICA_SCHEMA_PATH", defaultSQLiteDatabaseURL, defaultSQLiteSchemaPath))
		return 1
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = defaultSQLiteDatabaseURL
	}
	schemaPath := os.Getenv("MULTICA_SCHEMA_PATH")
	if schemaPath == "" {
		schemaPath = defaultSQLiteSchemaPath
	}
	if err := ensureSQLiteParentDir(dbURL); err != nil {
		slog.Error("migration failed", "error", err)
		return 1
	}
	dbConn, err := sqliteutil.Open(ctx, dbURL)
	if err != nil {
		slog.Error("migration failed", "error", err)
		return 1
	}
	defer dbConn.Close()

	slog.Info("applying sqlite schema", "database", dbURL, "schema", schemaPath)
	if err := sqliteutil.ApplySchema(ctx, dbConn, schemaPath); err != nil {
		slog.Error("migration failed", "error", err)
		return 1
	}
	fmt.Println("Done.")
	return 0
}

func resolveSiblingBin(name string) string {
	if _, err := os.Stat("./" + name); err == nil {
		return "./" + name
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "./" + name
}

func resolveWebBin() string {
	if raw := strings.TrimSpace(os.Getenv("MULTICA_WEB_BIN")); raw != "" {
		if _, err := os.Stat(raw); err == nil {
			return raw
		}
		return ""
	}
	for _, candidate := range []string{"/app/dist/server/multica-web", "./dist/server/multica-web"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func buildWebEnv(baseEnv []string) []string {
	apiPort := strings.TrimSpace(os.Getenv("PORT"))
	if apiPort == "" {
		apiPort = "8080"
	}
	webPort := strings.TrimSpace(os.Getenv("FRONTEND_PORT"))
	if webPort == "" {
		webPort = strings.TrimSpace(os.Getenv("WEB_PORT"))
	}
	if webPort == "" {
		webPort = "3000"
	}
	webHost := strings.TrimSpace(os.Getenv("FRONTEND_HOST"))
	if webHost == "" {
		webHost = "0.0.0.0"
	}
	remoteAPI := strings.TrimSpace(os.Getenv("REMOTE_API_URL"))
	if remoteAPI == "" {
		remoteAPI = "http://127.0.0.1:" + apiPort
	}

	out := make([]string, 0, len(baseEnv)+4)
	for _, kv := range baseEnv {
		if strings.HasPrefix(kv, "PORT=") ||
			strings.HasPrefix(kv, "HOST=") ||
			strings.HasPrefix(kv, "REMOTE_API_URL=") ||
			strings.HasPrefix(kv, "NODE_ENV=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"NODE_ENV=production",
		"HOST="+webHost,
		"PORT="+webPort,
		"REMOTE_API_URL="+remoteAPI,
	)
	return out
}

func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal())
			}
			return status.ExitStatus()
		}
	}
	return 1
}

// runEntrypoint implements the container entrypoint in pure Go so the single
// distroless container needs no /bin/sh:
//  1. Sets MULTICA_INTERNAL_DATABASE_STARTUP_STARTED_AT_UNIX.
//  2. Runs `./migrate up` with signal forwarding (143 on SIGTERM, 130 on SIGINT).
//  3. Starts `./server` and (unless MULTICA_DISABLE_WEB is set) `multica-web`,
//     forwarding SIGTERM/SIGINT to both and exiting when either exits.
func runEntrypoint(args []string) int {
	startedAt := strconv.FormatInt(time.Now().Unix(), 10)
	_ = os.Setenv("MULTICA_INTERNAL_DATABASE_STARTUP_STARTED_AT_UNIX", startedAt)

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	fmt.Println("Running database migrations...")
	migrateCmd := exec.Command(resolveSiblingBin("migrate"), "up")
	migrateCmd.Stdout = os.Stdout
	migrateCmd.Stderr = os.Stderr
	migrateCmd.Env = os.Environ()
	if err := migrateCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start migrate: %v\n", err)
		return 1
	}

	migrateDone := make(chan error, 1)
	go func() {
		migrateDone <- migrateCmd.Wait()
	}()

	select {
	case sig := <-sigCh:
		signal.Stop(sigCh)
		if migrateCmd.Process != nil {
			_ = migrateCmd.Process.Signal(sig)
		}
		<-migrateDone
		if sig == syscall.SIGINT {
			return 130
		}
		return 143
	case err := <-migrateDone:
		if err != nil {
			return exitCodeFromErr(err)
		}
	}

	type childProc struct {
		name string
		cmd  *exec.Cmd
	}
	var children []childProc

	disableWeb := strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_DISABLE_WEB")))
	if disableWeb != "1" && disableWeb != "true" {
		if webBin := resolveWebBin(); webBin != "" {
			fmt.Println("Starting web server...")
			webCmd := exec.Command(webBin)
			webCmd.Stdout = os.Stdout
			webCmd.Stderr = os.Stderr
			webCmd.Env = buildWebEnv(os.Environ())
			if err := webCmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to start web server: %v\n", err)
				return 1
			}
			children = append(children, childProc{name: "web", cmd: webCmd})
		}
	}

	fmt.Println("Starting server...")
	serverCmd := exec.Command(resolveSiblingBin("server"), args...)
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	serverCmd.Env = os.Environ()
	if err := serverCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start server: %v\n", err)
		for _, c := range children {
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Signal(syscall.SIGTERM)
			}
		}
		return 1
	}
	children = append(children, childProc{name: "server", cmd: serverCmd})

	type childExit struct {
		name string
		err  error
	}
	exitCh := make(chan childExit, len(children))
	for _, c := range children {
		c := c
		go func() {
			exitCh <- childExit{name: c.name, err: c.cmd.Wait()}
		}()
	}

	var firstSignal os.Signal
	var firstExit *childExit

	select {
	case sig := <-sigCh:
		firstSignal = sig
		for _, c := range children {
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Signal(sig)
			}
		}
	case ex := <-exitCh:
		firstExit = &ex
		for _, c := range children {
			if c.name != ex.name && c.cmd.Process != nil {
				_ = c.cmd.Process.Signal(syscall.SIGTERM)
			}
		}
	}

	signal.Stop(sigCh)
	remaining := len(children)
	if firstExit != nil {
		remaining--
	}
	timeout := time.After(20 * time.Second)
	for remaining > 0 {
		select {
		case ex := <-exitCh:
			if firstExit == nil || ex.err != nil {
				firstExit = &ex
			}
			remaining--
		case <-timeout:
			for _, c := range children {
				if c.cmd.Process != nil {
					_ = c.cmd.Process.Kill()
				}
			}
			remaining = 0
		}
	}

	if firstSignal == syscall.SIGINT {
		return 130
	}
	if firstSignal == syscall.SIGTERM {
		if firstExit != nil && firstExit.err == nil {
			return 0
		}
		return 143
	}
	if firstExit != nil {
		return exitCodeFromErr(firstExit.err)
	}
	return 0
}
