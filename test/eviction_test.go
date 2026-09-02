package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestReportEvictsPreviousConnection verifies that when a second agent
// connects with the same token, the first connection is evicted (closed)
// and no spurious offline event is published — the host stays online until
// the second connection closes.
func TestReportEvictsPreviousConnection(t *testing.T) {
	st, mux := newTestApp(t, nil)
	host, err := st.CreateHost("Evict-01", "token-evict", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"

	// Subscribe to the SSE overview stream to capture status transitions.
	sseResp, err := appSrv.Client().Get(appSrv.URL + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer sseResp.Body.Close()

	sseEvents := make(chan string, 32)
	go func() {
		buf := make([]byte, 1024)
		var pending string
		for {
			n, err := sseResp.Body.Read(buf)
			if n > 0 {
				pending += string(buf[:n])
				for {
					idx := strings.Index(pending, "\n\n")
					if idx < 0 {
						break
					}
					block := pending[:idx]
					pending = pending[idx+2:]
					if line, ok := strings.CutPrefix(block, "data: "); ok {
						select {
						case sseEvents <- line:
						default:
						}
					}
				}
			}
			if err != nil {
				close(sseEvents)
				return
			}
		}
	}()

	expectSSE := func(t *testing.T, want string) {
		t.Helper()
		for {
			select {
			case ev, ok := <-sseEvents:
				if !ok {
					t.Fatalf("SSE stream closed; waiting for %s", want)
				}
				if strings.Contains(ev, want) {
					return
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("timeout waiting for SSE event containing %s", want)
			}
		}
	}

	requireOnline := func(t *testing.T, want bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/hosts status = %d", rec.Code)
			}
			var hosts []struct {
				Online bool `json:"online"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&hosts); err != nil {
				t.Fatalf("decode hosts: %v", err)
			}
			if len(hosts) == 1 && hosts[0].Online == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("host online status did not reach %v", want)
	}

	tokenHeader := http.Header{"Token": []string{host.Token}}

	// --- Connect first agent: host transitions to online. ---
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, tokenHeader)
	if err != nil {
		t.Fatalf("dial conn1: %v", err)
	}
	defer conn1.Close()
	expectSSE(t, `"online":true`)
	requireOnline(t, true)

	// --- Connect second agent: evicts conn1, host stays online. ---
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, tokenHeader)
	if err != nil {
		t.Fatalf("dial conn2: %v", err)
	}
	defer conn2.Close()

	// conn1 should have been closed by the server (eviction).
	if err := conn1.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("conn1 SetReadDeadline: %v", err)
	}
	if _, _, err := conn1.ReadMessage(); err == nil {
		t.Fatal("conn1 was not evicted: ReadMessage returned nil after conn2 connected")
	}

	// No spurious offline event should arrive between eviction and conn2 close.
	select {
	case ev := <-sseEvents:
		t.Fatalf("unexpected SSE event during eviction (host should stay online): %s", ev)
	case <-time.After(200 * time.Millisecond):
		// good — no event
	}
	requireOnline(t, true)

	// --- Close second agent: host transitions to offline. ---
	if err := conn2.Close(); err != nil {
		t.Fatalf("conn2 Close: %v", err)
	}
	expectSSE(t, `"online":false`)
	requireOnline(t, false)
}
