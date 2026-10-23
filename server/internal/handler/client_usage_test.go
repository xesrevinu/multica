package handler

import (
	"testing"

)

func ptrInt32(value int32) *int32 { return &value }

func TestValidateClientUsageRuntime(t *testing.T) {
	tests := []struct {
		name    string
		probe   clientUsageRuntimeProbe
		wantErr bool
	}{
		{name: "probe error", probe: clientUsageRuntimeProbe{ProbeResult: "error"}},
		{name: "successful empty config", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(0), ProviderSummary: map[string]int{}, OnlineCount: ptrInt32(0), OfflineCount: ptrInt32(0)}},
		{name: "successful mixed config", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(3), ProviderSummary: map[string]int{"claude": 2, "codex": 1}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(2)}},
		{name: "error with counts", probe: clientUsageRuntimeProbe{ProbeResult: "error", RuntimeCount: ptrInt32(0)}, wantErr: true},
		{name: "missing success fields", probe: clientUsageRuntimeProbe{ProbeResult: "success"}, wantErr: true},
		{name: "state total mismatch", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(2), ProviderSummary: map[string]int{"codex": 2}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(0)}, wantErr: true},
		{name: "provider total mismatch", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(2), ProviderSummary: map[string]int{"codex": 1}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(1)}, wantErr: true},
		{name: "unsafe provider", probe: clientUsageRuntimeProbe{ProbeResult: "success", RuntimeCount: ptrInt32(1), ProviderSummary: map[string]int{"Codex Pro": 1}, OnlineCount: ptrInt32(1), OfflineCount: ptrInt32(0)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateClientUsageRuntime(tt.probe)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateClientUsageRuntime() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeClientUsageOS(t *testing.T) {
	if got := normalizeClientUsageOS(" MacOS "); got != "macos" {
		t.Fatalf("normalizeClientUsageOS() = %q, want macos", got)
	}
	if got := normalizeClientUsageOS("Darwin 24.4"); got != "unknown" {
		t.Fatalf("normalizeClientUsageOS() = %q, want unknown", got)
	}
}

