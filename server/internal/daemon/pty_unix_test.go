//go:build !windows

package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestStartPTYEchoesCommand(t *testing.T) {
	dir := t.TempDir()
	session, err := startPTY(protocol.PTYControl{
		Cols: 80,
		Rows: 24,
		Cwd:  dir,
		Argv: []string{"/bin/sh", "-c", "printf 'pty-ok\\n'"},
	})
	if err != nil {
		t.Skipf("startPTY unavailable: %v", err)
	}
	defer session.close()

	deadline := time.Now().Add(3 * time.Second)
	var buf bytes.Buffer
	scratch := make([]byte, 1024)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		file := session.file
		session.mu.Unlock()
		if file == nil {
			break
		}
		if f, ok := file.(*os.File); ok {
			_ = f.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		}
		n, err := file.Read(scratch)
		if n > 0 {
			buf.Write(scratch[:n])
			if bytes.Contains(buf.Bytes(), []byte("pty-ok")) {
				return
			}
		}
		if err != nil && !os.IsTimeout(err) {
			break
		}
	}
	t.Fatalf("did not see pty-ok, got %q", buf.Bytes())
}

func TestResolvePTYCwdHome(t *testing.T) {
	got, err := resolvePTYCwd("")
	if err != nil {
		t.Fatalf("resolvePTYCwd: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got != home {
		t.Fatalf("cwd = %q, want home %q", got, home)
	}
}

func TestResolvePTYCwdExplicit(t *testing.T) {
	dir := t.TempDir()
	got, err := resolvePTYCwd(dir)
	if err != nil {
		t.Fatalf("resolvePTYCwd: %v", err)
	}
	if got != dir {
		t.Fatalf("cwd = %q, want %q", got, dir)
	}
	if _, err := os.Stat(filepath.Join(dir)); err != nil {
		t.Fatalf("temp dir missing: %v", err)
	}
}
