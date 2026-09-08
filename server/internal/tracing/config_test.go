package tracing

import "testing"

func TestConfigEmptyIsDisabled(t *testing.T) {
	t.Setenv("MULTICA_OTEL_ENDPOINT", "")
	t.Setenv("MULTICA_OTEL_SERVICE_NAME", "")
	cfg := ConfigFromEnv()
	if cfg.Enabled() {
		t.Fatal("empty endpoint must disable tracing")
	}
	if cfg.ServiceName != defaultServiceName {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, defaultServiceName)
	}
}

func TestConfigWhitespaceIsDisabled(t *testing.T) {
	t.Setenv("MULTICA_OTEL_ENDPOINT", "  ")
	if ConfigFromEnv().Enabled() {
		t.Fatal("whitespace endpoint must disable tracing")
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("MULTICA_OTEL_ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("MULTICA_OTEL_SERVICE_NAME", "multica-api-test")
	cfg := ConfigFromEnv()
	if !cfg.Enabled() {
		t.Fatal("endpoint must enable tracing")
	}
	if cfg.Endpoint != "http://127.0.0.1:4318" {
		t.Fatalf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.ServiceName != "multica-api-test" {
		t.Fatalf("ServiceName = %q", cfg.ServiceName)
	}
}

func TestTracesURLAppendsPath(t *testing.T) {
	got := tracesURL("http://127.0.0.1:4318")
	if got != "http://127.0.0.1:4318/v1/traces" {
		t.Fatalf("tracesURL = %q", got)
	}
	got = tracesURL("http://127.0.0.1:4318/v1/traces")
	if got != "http://127.0.0.1:4318/v1/traces" {
		t.Fatalf("tracesURL preserved path = %q", got)
	}
}
