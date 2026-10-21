package metrics

import "testing"

func TestNormalizeRuntimeProviderRecognizesKnownProviders(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "Claude", want: "claude"},
		{input: "CODEX", want: "codex"},
		{input: "OpenCode", want: "opencode"},
		{input: "Pi", want: "pi"},
		{input: "OMP", want: "omp"},
		{input: "DSH", want: "dsh"},
		{input: "Grok", want: "grok"},
		{input: "gbot", want: "gbot"},
		{input: "Yielded", want: "yielded"},
	}
	for _, tt := range tests {
		if got := NormalizeRuntimeProvider(tt.input); got != tt.want {
			t.Errorf("NormalizeRuntimeProvider(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeLabelsCollapseUnknownValues(t *testing.T) {
	if got := NormalizeRuntimeProvider("provider-from-user-input"); got != "other" {
		t.Fatalf("NormalizeRuntimeProvider unknown = %q, want other", got)
	}
	if got := NormalizeRuntimeProvider("hermes"); got != "other" {
		t.Fatalf("NormalizeRuntimeProvider removed provider = %q, want other", got)
	}
	if got := NormalizeRuntimeMode("workspace-123"); got != "unknown" {
		t.Fatalf("NormalizeRuntimeMode unknown = %q, want unknown", got)
	}
	if got := NormalizeTaskSource("task-123"); got != "other" {
		t.Fatalf("NormalizeTaskSource unknown = %q, want other", got)
	}
}
