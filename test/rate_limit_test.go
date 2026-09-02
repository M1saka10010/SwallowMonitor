package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/gorilla/websocket"
)

// TestReportRateLimit verifies that /report rejects requests with 429 after
// the per-IP limit is reached, regardless of whether the token is valid.
func TestReportRateLimit(t *testing.T) {
	st, mux := newTestApp(t, &model.Config{ReportRateLimit: 3})
	host, err := st.CreateHost("Rate-01", "token-rate", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"

	// Exhaust the limit with invalid tokens (each returns 401 but still
	// counts against the rate limiter).
	badHeader := http.Header{"Token": []string{"wrong"}}
	for i := 0; i < 3; i++ {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, badHeader)
		if err == nil {
			t.Fatalf("attempt %d with bad token: expected dial failure", i)
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("attempt %d: status = %d, want %d", i, status, http.StatusUnauthorized)
		}
	}

	// 4th attempt — even with a valid token — should be rate limited.
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Token": []string{host.Token}})
	if err == nil {
		t.Fatal("4th attempt: expected dial failure (rate limited)")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("4th attempt: status = %d, want %d", status, http.StatusTooManyRequests)
	}
}

// TestReportRateLimitWithTrustedProxy verifies that when the server runs
// behind a trusted reverse proxy, the rate limit is applied per real client
// IP (from X-Forwarded-For), not per proxy address.
func TestReportRateLimitWithTrustedProxy(t *testing.T) {
	cfg := &model.Config{
		ReportRateLimit: 3,
		TrustedProxies:  []string{"127.0.0.0/8"},
	}
	st, mux := newTestApp(t, cfg)
	host, err := st.CreateHost("Proxy-01", "token-proxy", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"

	// The test server runs on 127.0.0.1, which is in the trusted range.
	// Requests carry X-Forwarded-For to simulate clients behind the proxy.
	badToken := http.Header{
		"Token":            []string{"wrong"},
		"X-Forwarded-For":  []string{"203.0.113.1"},
	}

	// First 3 attempts from 203.0.113.1: 401 (bad token, within rate).
	for i := 0; i < 3; i++ {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, badToken)
		if err == nil {
			t.Fatalf("attempt %d: expected dial failure", i)
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("attempt %d: status = %d, want 401", i, status)
		}
	}

	// 4th from 203.0.113.1: 429 (rate limited for this real IP).
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Token":           []string{host.Token},
		"X-Forwarded-For": []string{"203.0.113.1"},
	})
	if err == nil {
		t.Fatal("4th attempt from 203.0.113.1: expected rate limited")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("4th attempt: status = %d, want 429", status)
	}

	// A different real IP is NOT rate limited (separate bucket).
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"Token":           []string{host.Token},
		"X-Forwarded-For": []string{"203.0.113.2"},
	})
	if err != nil {
		t.Fatalf("different IP should connect: err=%v", err)
	}
	defer conn.Close()
}
