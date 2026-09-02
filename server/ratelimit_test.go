package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPRateLimiterAllowAndBlock(t *testing.T) {
	rl := newIPRateLimiter(3, 100*time.Millisecond)
	ip := "192.0.2.1"

	for i := 0; i < 3; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("attempt %d: expected allowed", i)
		}
	}
	if rl.Allow(ip) {
		t.Fatal("4th attempt: expected blocked")
	}
}

func TestIPRateLimiterPerIPIsolation(t *testing.T) {
	rl := newIPRateLimiter(1, 100*time.Millisecond)

	if !rl.Allow("192.0.2.1") {
		t.Fatal("first IP first attempt: expected allowed")
	}
	if rl.Allow("192.0.2.1") {
		t.Fatal("first IP second attempt: expected blocked")
	}
	// A different IP is not affected.
	if !rl.Allow("192.0.2.2") {
		t.Fatal("second IP: expected allowed")
	}
}

func TestIPRateLimiterWindowReset(t *testing.T) {
	rl := newIPRateLimiter(2, 50*time.Millisecond)
	ip := "192.0.2.1"

	rl.Allow(ip)
	rl.Allow(ip)
	if rl.Allow(ip) {
		t.Fatal("3rd attempt within window: expected blocked")
	}

	time.Sleep(60 * time.Millisecond)
	if !rl.Allow(ip) {
		t.Fatal("after window expiry: expected allowed")
	}
}

func TestIPRateLimiterLazyCleanup(t *testing.T) {
	rl := newIPRateLimiter(1, time.Second)

	// Directly populate the map with stale entries (before the window cutoff)
	// so the single Allow call below triggers lazy cleanup.
	stale := time.Now().Add(-2 * time.Second)
	rl.mu.Lock()
	for i := 0; i < cleanupThreshold+50; i++ {
		rl.counts[fmt.Sprintf("10.0.0.%d", i)] = []time.Time{stale}
	}
	rl.mu.Unlock()

	// The map is over the threshold; Allow should purge stale IPs and keep
	// only the one we just added.
	rl.Allow("10.0.0.250")

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if len(rl.counts) > 2 {
		t.Fatalf("lazy cleanup left %d entries, want <= 2", len(rl.counts))
	}
}

// --- trusted proxy / clientIP tests ---

func TestParseTrustedProxies(t *testing.T) {
	tests := []struct {
		input   string
		wantOK  bool
		wantNet string
	}{
		{"127.0.0.1", true, "127.0.0.1/32"},
		{"10.0.0.0/8", true, "10.0.0.0/8"},
		{"::1", true, "::1/128"},
		{"not-an-ip", false, ""},
		{"  ", false, ""},
	}
	for _, tt := range tests {
		nets := parseTrustedProxies([]string{tt.input})
		gotOK := len(nets) == 1
		if gotOK != tt.wantOK {
			t.Errorf("parseTrustedProxies(%q) ok=%v, want %v", tt.input, gotOK, tt.wantOK)
			continue
		}
		if gotOK && nets[0].String() != tt.wantNet {
			t.Errorf("parseTrustedProxies(%q) = %s, want %s", tt.input, nets[0].String(), tt.wantNet)
		}
	}
}

func TestClientIPNoTrustedProxiesUsesRemoteAddr(t *testing.T) {
	s := &Server{} // no trusted proxies configured
	r := httptest.NewRequest(http.MethodGet, "/report", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	// XFF is ignored because the source is not a trusted proxy.
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := s.clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP = %q, want 203.0.113.5", got)
	}
}

func TestClientIPTrustedProxyUsesXFF(t *testing.T) {
	s := &Server{trustedProxies: parseTrustedProxies([]string{"127.0.0.0/8"})}
	r := httptest.NewRequest(http.MethodGet, "/report", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	if got := s.clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP = %q, want 203.0.113.5", got)
	}
}

func TestClientIPUntrustedSourceIgnoresXFF(t *testing.T) {
	s := &Server{trustedProxies: parseTrustedProxies([]string{"127.0.0.0/8"})}
	r := httptest.NewRequest(http.MethodGet, "/report", nil)
	r.RemoteAddr = "198.51.100.1:54321" // not a trusted proxy
	r.Header.Set("X-Forwarded-For", "203.0.113.5") // forged
	if got := s.clientIP(r); got != "198.51.100.1" {
		t.Fatalf("clientIP = %q, want 198.51.100.1 (forged XFF from untrusted source should be ignored)", got)
	}
}

func TestClientIPMultiHopSkipsTrustedProxies(t *testing.T) {
	// Client → 10.0.0.1 (trusted) → 10.0.0.2 (trusted) → server
	s := &Server{trustedProxies: parseTrustedProxies([]string{"10.0.0.0/8"})}
	r := httptest.NewRequest(http.MethodGet, "/report", nil)
	r.RemoteAddr = "10.0.0.2:54321"
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	// Walk right-to-left: 10.0.0.1 (trusted, skip) → 203.0.113.5 (real client)
	if got := s.clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP = %q, want 203.0.113.5", got)
	}
}

func TestClientIPForgedXFFRightmostIsReal(t *testing.T) {
	// Client forges XFF: "1.2.3.4". Proxy appends real client: "203.0.113.5".
	// Final XFF: "1.2.3.4, 203.0.113.5". The forged left entry is ignored.
	s := &Server{trustedProxies: parseTrustedProxies([]string{"127.0.0.0/8"})}
	r := httptest.NewRequest(http.MethodGet, "/report", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.5")
	if got := s.clientIP(r); got != "203.0.113.5" {
		t.Fatalf("clientIP = %q, want 203.0.113.5 (forged left entry should be ignored)", got)
	}
}
