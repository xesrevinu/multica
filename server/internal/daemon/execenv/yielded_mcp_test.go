package execenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareYieldedMcpConfigWritesWorkspaceFile(t *testing.T) {
	workDir := t.TempDir()
	manifest := &sidecarManifest{}
	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`)

	if err := prepareYieldedMcpConfig(workDir, "yielded", raw, manifest); err != nil {
		t.Fatalf("prepareYieldedMcpConfig: %v", err)
	}
	path := filepath.Join(workDir, ".mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(raw)+"\n" {
		t.Fatalf("config = %s", data)
	}
	if mode := filePerm(t, path); mode != 0o600 {
		t.Fatalf(".mcp.json mode = %#o, want 0600", mode)
	}
	if !containsPath(manifest.Files, path) {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestPrepareYieldedMcpConfigNullLeavesFileAlone(t *testing.T) {
	workDir := t.TempDir()
	if err := prepareYieldedMcpConfig(workDir, "yielded", nil, &sidecarManifest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf(".mcp.json should not exist, stat err=%v", err)
	}
}

func TestPrepareYieldedMcpConfigRefusesExistingFile(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, ".mcp.json")
	existing := []byte(`{"mcpServers":{"user":{"command":"echo"}}}` + "\n")
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	err := prepareYieldedMcpConfig(workDir, "yielded", json.RawMessage(`{"mcpServers":{"managed":{"command":"uvx"}}}`), &sidecarManifest{})
	if err == nil || !strings.Contains(err.Error(), "would overwrite") {
		t.Fatalf("error = %v, want overwrite error", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != string(existing) {
		t.Fatalf("existing config changed to %s", data)
	}
}

func TestPrepareYieldedMcpConfigRejectsBadShape(t *testing.T) {
	workDir := t.TempDir()
	err := prepareYieldedMcpConfig(workDir, "yielded", json.RawMessage(`{"mcpServers":["nope"]}`), &sidecarManifest{})
	if err == nil {
		t.Fatal("expected mcpServers array to be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(workDir, ".mcp.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected config was written, stat err=%v", statErr)
	}
}

func TestPrepareYieldedMcpConfigIgnoresOtherProviders(t *testing.T) {
	workDir := t.TempDir()
	if err := prepareYieldedMcpConfig(workDir, "pi", json.RawMessage(`{"mcpServers":{}}`), &sidecarManifest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf(".mcp.json should not exist for pi, stat err=%v", err)
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
