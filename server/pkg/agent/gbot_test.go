package agent

import (
	"testing"
	"time"
)

func TestGbotResultMapsTerminalStates(t *testing.T) {
	completed := gbotResult([]byte(`{"status":"completed","sessionId":"m1","reply":"done","exitCode":0}`), nil, nil, time.Second)
	if completed.Status != "completed" || completed.SessionID != "m1" || completed.Output != "done" {
		t.Fatalf("completed = %#v", completed)
	}
	unknown := gbotResult([]byte(`{"status":"unknown","sessionId":"m1","reply":"","exitCode":1}`), nil, nil, time.Second)
	if unknown.Status != "failed" || unknown.Error == "" {
		t.Fatalf("unknown = %#v", unknown)
	}
	missing := gbotResult([]byte("not-json"), nil, nil, time.Second)
	if missing.Status != "failed" || missing.Error != "gbot returned no task result" {
		t.Fatalf("missing = %#v", missing)
	}
}

func TestGbotIsADistinctRuntime(t *testing.T) {
	if !IsSupportedType("gbot") {
		t.Fatal("gbot is not a supported runtime")
	}
	if LaunchHeader("gbot") != "gbot task run" {
		t.Fatalf("header = %q", LaunchHeader("gbot"))
	}
	backend, err := New("gbot", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.(*gbotBackend); !ok {
		t.Fatalf("backend = %T", backend)
	}
}
