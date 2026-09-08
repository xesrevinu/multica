package tracing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSetupExportsOTLPHTTPWithoutGzip(t *testing.T) {
	var (
		mu      sync.Mutex
		gotPath string
		gotEnc  string
		gotType string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotEnc = r.Header.Get("Content-Encoding")
		gotType = r.Header.Get("Content-Type")
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partialSuccess":{}}`))
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	stop, err := Setup(ctx, Config{Endpoint: srv.URL, ServiceName: "multica-api-test"}, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = stop(context.Background()) })

	_, span := tracer().Start(ctx, "probe")
	span.End()

	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := stop(flushCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/v1/traces" {
		t.Fatalf("path = %q, want /v1/traces", gotPath)
	}
	if gotEnc != "" && !strings.EqualFold(gotEnc, "identity") {
		t.Fatalf("Content-Encoding = %q, want empty (no gzip)", gotEnc)
	}
	if !strings.Contains(gotType, "protobuf") && !strings.Contains(gotType, "json") {
		t.Fatalf("Content-Type = %q, want otlp protobuf or json", gotType)
	}
}

func TestSetupDisabledIsNoop(t *testing.T) {
	stop, err := Setup(context.Background(), Config{}, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
