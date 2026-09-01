package daemon

import "testing"

func TestPTYWebSocketURL(t *testing.T) {
	got, err := ptyWebSocketURL("https://api.example.com")
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if got != "wss://api.example.com/api/daemon/pty/ws" {
		t.Fatalf("got %q", got)
	}

	got, err = ptyWebSocketURL("http://localhost:8080")
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if got != "ws://localhost:8080/api/daemon/pty/ws" {
		t.Fatalf("got %q", got)
	}
}
