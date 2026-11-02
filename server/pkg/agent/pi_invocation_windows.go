//go:build windows

package agent

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// powerShellLookup resolves the PowerShell host. Tests replace it.
var powerShellLookup = defaultPowerShellLookup

func defaultPowerShellLookup() (string, bool) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	candidate := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, true
	}
	return "", false
}

// platformPiInvocation rewrites pi.cmd → PowerShell -Command pi.ps1 on
// Windows. -Command with @args is the only route that preserves stdin bytes:
// -File re-encodes stdin under the console ANSI codepage, destroying
// non-ASCII input (#7355). It also avoids cmd.exe %* re-tokenisation (#3306).
func platformPiInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1Command("pi", lookedUp, args, logger)
}

func rewriteCmdToPS1Command(toolName, lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	ext := strings.ToLower(filepath.Ext(lookedUp))
	if ext != ".cmd" && ext != ".bat" {
		return "", nil, false
	}
	ps1 := filepath.Join(filepath.Dir(lookedUp), toolName+".ps1")
	if st, err := os.Stat(ps1); err != nil || st.IsDir() {
		return "", nil, false
	}

	psExe, ok := powerShellLookup()
	if !ok {
		return "", nil, false
	}

	full := piPowerShellCommandArgs(ps1, args)

	if logger != nil {
		logger.Info(toolName+": routing through powershell -Command to preserve stdin",
			"powershell", psExe,
			"ps1", ps1,
			"original", lookedUp,
		)
	}
	return psExe, full, true
}
