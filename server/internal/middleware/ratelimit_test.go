package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRateLimit_FailOpen(t *testing.T) {
	mw := RateLimit(5, time.Minute, nil)
	handler := mw(okHandler)

	for i := 0; i < 8; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("limiter should pass through; got status %d", rec.Code)
		}
	}
}

func mustParseCIDR(cidr string) *net.IPNet {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return n
}

func TestExtractIP_NoTrustedProxies_IgnoresXFF(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
	req.RemoteAddr = "203.0.113.1:9000"
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.1")

	ip := extractIP(req, nil)
	if ip != "203.0.113.1" {
		t.Fatalf("with no trusted proxies, expected RemoteAddr; got %q", ip)
	}
}

func TestExtractIP_TrustedProxy_HonorsXFF(t *testing.T) {
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}

	req := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
	req.RemoteAddr = "10.0.0.1:9000"
	req.Header.Set("X-Forwarded-For", "198.51.100.42, 10.0.0.5")

	ip := extractIP(req, trusted)
	if ip != "198.51.100.42" {
		t.Fatalf("expected rightmost non-trusted IP; got %q", ip)
	}
}

func TestExtractIP_UntrustedSource_IgnoresXFF(t *testing.T) {
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}

	req := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
	req.RemoteAddr = "203.0.113.99:9000"
	req.Header.Set("X-Forwarded-For", "198.51.100.1")

	ip := extractIP(req, trusted)
	if ip != "203.0.113.99" {
		t.Fatalf("untrusted source should use RemoteAddr; got %q", ip)
	}
}

func TestExtractIP_IPv6Normalization(t *testing.T) {
	req1 := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
	req1.RemoteAddr = "[::1]:9000"

	req2 := httptest.NewRequest(http.MethodPost, "/auth/send-code", nil)
	req2.RemoteAddr = "[0:0:0:0:0:0:0:1]:9000"

	ip1 := extractIP(req1, nil)
	ip2 := extractIP(req2, nil)
	if ip1 != ip2 {
		t.Fatalf("IPv6 representations should normalize to same key: %q vs %q", ip1, ip2)
	}
}

func TestParseTrustedProxies_Empty(t *testing.T) {
	if nets := ParseTrustedProxies(""); nets != nil {
		t.Fatalf("empty string should return nil, got %v", nets)
	}
	if nets := ParseTrustedProxies("  "); nets != nil {
		t.Fatalf("whitespace should return nil, got %v", nets)
	}
}

func TestParseTrustedProxies_Valid(t *testing.T) {
	nets := ParseTrustedProxies("10.0.0.0/8, 172.16.0.0/12")
	if len(nets) != 2 {
		t.Fatalf("expected 2 CIDRs, got %d", len(nets))
	}
}

func TestParseTrustedProxies_InvalidSkipped(t *testing.T) {
	nets := ParseTrustedProxies("10.0.0.0/8, not-a-cidr, 172.16.0.0/12")
	if len(nets) != 2 {
		t.Fatalf("expected 2 valid CIDRs (invalid skipped), got %d", len(nets))
	}
}
