package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
)

func securityServer(t *testing.T) (*Server, string) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	host, err := st.CreateHost("test", "security-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := New(&model.Config{}, st, http.NewServeMux(), http.NotFoundHandler())
	return s, host.PublicID
}

func usageMessage(ts uint64) []byte {
	b, _ := json.Marshal(map[string]any{"type": "system_usage", "data": map[string]any{"timestamp": ts}})
	return b
}

func TestSecurityUsageTimestamp(t *testing.T) {
	s, id := securityServer(t)
	sub := s.hub.SubscribeOverview()
	defer s.hub.UnsubscribeOverview(sub)
	for _, ts := range []uint64{1, uint64(time.Now().Add(time.Hour).Unix()), ^uint64(0)} {
		s.dispatch(id, usageMessage(ts))
		u, err := s.store.LatestUsage(id)
		if err != nil || u != nil {
			t.Fatalf("invalid timestamp persisted: %v %v", u, err)
		}
		if len(sub.ch) != 0 {
			t.Fatal("invalid usage published")
		}
	}
	before := time.Now().Unix()
	s.dispatch(id, usageMessage(uint64(before+120)))
	host, err := s.store.GetHost(id)
	if err != nil {
		t.Fatal(err)
	}
	if host.LastSeen < before || host.LastSeen > time.Now().Unix() {
		t.Fatalf("last_seen trusts agent: %d", host.LastSeen)
	}
	s.dispatch(id, usageMessage(0))
	if len(sub.ch) != 2 {
		t.Fatal("valid usage not published")
	}
}

func TestSecurityReportLimits(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		t.Run(fmt.Sprint(oversized), func(t *testing.T) {
			s, id := securityServer(t)
			ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
			defer ts.Close()
			c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			defer s.cancelOfflineNotification(id)
			if oversized {
				_ = c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maxReportMessageSize+1)))
			} else {
				for i := 0; i <= reportMessagesPerMinute; i++ {
					if err := c.WriteMessage(websocket.TextMessage, []byte("{}")); err != nil {
						break
					}
				}
			}
			c.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _, err = c.ReadMessage()
			want := websocket.ClosePolicyViolation
			if oversized {
				want = websocket.CloseMessageTooBig
			}
			if !websocket.IsCloseError(err, want) {
				t.Fatalf("close = %v, want %d", err, want)
			}
			if !oversized {
				retry, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
				if err != nil {
					t.Fatal(err)
				}
				defer retry.Close()
				_ = retry.WriteMessage(websocket.TextMessage, []byte("{}"))
				_ = retry.SetReadDeadline(time.Now().Add(time.Second))
				_, _, err = retry.ReadMessage()
				if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
					t.Fatalf("reconnect reset budget: %v", err)
				}
			}
		})
	}
}

func TestSecurityReportControlFramesShareHostBudget(t *testing.T) {
	s, id := securityServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()

	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer s.cancelOfflineNotification(id)

	for i := 0; i < reportMessagesPerMinute-1; i++ {
		if err := c.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		_, _, err = c.ReadMessage()
		if err != nil {
			break
		}
	}
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("close = %v, want %d", err, websocket.ClosePolicyViolation)
	}
}

func TestSecurityReportCloseFrameConsumesHostBudget(t *testing.T) {
	s, id := securityServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()

	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer s.cancelOfflineNotification(id)
	if err := c.WriteMessage(websocket.TextMessage, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, _ = c.ReadMessage()

	s.hub.messageLimiter.mu.Lock()
	consumed := len(s.hub.messageLimiter.counts[id])
	s.hub.messageLimiter.mu.Unlock()
	if consumed != 2 {
		t.Fatalf("budget consumed = %d, want 2 for data and close frames", consumed)
	}
}

func readStatus(t *testing.T, sub *subscriber) bool {
	t.Helper()
	select {
	case raw := <-sub.ch:
		var event struct {
			Type   string `json:"type"`
			Online bool   `json:"online"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "status" {
			t.Fatalf("event type = %q, want status", event.Type)
		}
		return event.Online
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for status event")
		return false
	}
}

func TestSecurityReconnectPublishesOnlineAfterPublishedOffline(t *testing.T) {
	s, id := securityServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()
	sub := s.hub.SubscribeOverview()
	defer s.hub.UnsubscribeOverview(sub)
	defer s.cancelOfflineNotification(id)
	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	first := dial()
	if !readStatus(t, sub) {
		t.Fatal("initial status is offline")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if readStatus(t, sub) {
		t.Fatal("disconnect status is online")
	}

	second := dial()
	defer second.Close()
	if !readStatus(t, sub) {
		t.Fatal("reconnect status is offline")
	}
}

func TestSecurityReplacementDoesNotPublishStaleOffline(t *testing.T) {
	s, id := securityServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()
	sub := s.hub.SubscribeOverview()
	defer s.hub.UnsubscribeOverview(sub)
	defer s.cancelOfflineNotification(id)
	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	first := dial()
	defer first.Close()
	if !readStatus(t, sub) {
		t.Fatal("initial status is offline")
	}
	second := dial()
	defer second.Close()

	select {
	case raw := <-sub.ch:
		t.Fatalf("replacement published stale or duplicate status: %s", raw)
	case <-time.After(100 * time.Millisecond):
	}
	if !s.hub.IsOnline(id) {
		t.Fatal("replacement connection is not online")
	}
}
