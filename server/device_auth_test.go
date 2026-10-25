package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type blockedAckConn struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
}

func (c *blockedAckConn) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("auth_ok")) {
		close(c.entered)
		<-c.release
		return 0, errors.New("test acknowledgement failure")
	}
	return c.Conn.Write(p)
}

type blockedAckListener struct {
	net.Listener
	entered chan struct{}
	release chan struct{}
}

func (l *blockedAckListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &blockedAckConn{Conn: c, entered: l.entered, release: l.release}, nil
}

func TestDeviceAcknowledgementDoesNotBlockRevocation(t *testing.T) {
	s, id := securityServer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ts := httptest.NewUnstartedServer(http.HandlerFunc(s.handleReport))
	ts.Listener = &blockedAckListener{Listener: ts.Listener, entered: entered, release: release}
	ts.Start()
	defer ts.Close()
	defer unblock()
	defer s.cancelOfflineNotification(id)
	key := reportTestPublicKey("security-token")
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"X-Agent-Public-Key": {key}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := reportTestProof(c, key); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("ack not attempted")
	}
	if !s.hub.IsOnline(id) {
		t.Fatal("proved device not registered before ack")
	}
	done := make(chan error, 1)
	go func() { done <- s.hub.RevokeAgent(id, func() error { return s.store.DeleteHost(id) }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ack held lifecycle lock")
	}
	unblock()
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("failed ack accepted")
	}
	if s.hub.IsOnline(id) {
		t.Fatal("revoked device online after failed ack")
	}
}

func freshDeviceKey(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub), priv
}

func readDeviceChallenge(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var challenge struct{ Type, Challenge string }
	if err := c.ReadJSON(&challenge); err != nil {
		t.Fatal(err)
	}
	nonce, err := base64.StdEncoding.Strict().DecodeString(challenge.Challenge)
	if err != nil || len(nonce) != 32 || challenge.Type != "auth_challenge" {
		t.Fatalf("invalid challenge: %+v", challenge)
	}
	return challenge.Challenge
}

func deviceProof(priv ed25519.PrivateKey, key, nonce string) map[string]string {
	sig := ed25519.Sign(priv, []byte("swallow-report-auth-v1\n"+key+"\n"+nonce))
	return map[string]string{"type": "auth_response", "signature": base64.StdEncoding.EncodeToString(sig)}
}

func authTestConnection(t *testing.T, key string, timeout time.Duration) (*websocket.Conn, <-chan error) {
	t.Helper()
	result := make(chan error, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			result <- err
			return
		}
		defer c.Close()
		result <- authenticateDeviceBefore(c, key, time.Now().Add(timeout))
	}))
	t.Cleanup(ts.Close)
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, result
}

func authResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("authentication did not terminate")
		return nil
	}
}

func TestDeviceAuthenticationProof(t *testing.T) {
	for _, name := range []string{"success", "key", "domain", "nonce", "type"} {
		t.Run(name, func(t *testing.T) {
			key, priv := freshDeviceKey(t)
			c, result := authTestConnection(t, key, time.Second)
			nonce := readDeviceChallenge(t, c)
			proof := deviceProof(priv, key, nonce)
			switch name {
			case "key":
				_, other := freshDeviceKey(t)
				proof = deviceProof(other, key, nonce)
			case "domain":
				proof["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("wrong-domain\n"+key+"\n"+nonce)))
			case "nonce":
				proof = deviceProof(priv, key, base64.StdEncoding.EncodeToString(make([]byte, 32)))
			case "type":
				proof["type"] = "system_usage"
			}
			if err := c.WriteJSON(proof); err != nil {
				t.Fatal(err)
			}
			if err := authResult(t, result); (err == nil) != (name == "success") {
				t.Fatalf("authentication = %v", err)
			}
		})
	}
}

func TestDeviceAuthenticationReplay(t *testing.T) {
	key, priv := freshDeviceKey(t)
	first, r1 := authTestConnection(t, key, time.Second)
	nonce := readDeviceChallenge(t, first)
	proof := deviceProof(priv, key, nonce)
	if err := first.WriteJSON(proof); err != nil {
		t.Fatal(err)
	}
	if err := authResult(t, r1); err != nil {
		t.Fatal(err)
	}
	second, r2 := authTestConnection(t, key, time.Second)
	if next := readDeviceChallenge(t, second); next == nonce {
		t.Fatal("challenge reused")
	}
	if err := second.WriteJSON(proof); err != nil {
		t.Fatal(err)
	}
	if err := authResult(t, r2); err == nil {
		t.Fatal("cross-connection replay accepted")
	}
}

func TestDeviceAuthenticationLimits(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		key, _ := freshDeviceKey(t)
		c, result := authTestConnection(t, key, 80*time.Millisecond)
		readDeviceChallenge(t, c)
		_ = c.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second))
		err := authResult(t, result)
		if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			t.Fatalf("expected timeout, got %v", err)
		}
	})
	t.Run("oversized", func(t *testing.T) {
		key, _ := freshDeviceKey(t)
		c, result := authTestConnection(t, key, time.Second)
		readDeviceChallenge(t, c)
		_ = c.WriteJSON(map[string]string{"type": "auth_response", "signature": strings.Repeat("x", 4097)})
		if err := authResult(t, result); err != websocket.ErrReadLimit {
			t.Fatalf("expected read limit, got %v", err)
		}
	})
	for _, count := range []int{8, 9} {
		name := "allowed"
		if count == 9 {
			name = "exceeded"
		}
		t.Run("controls_"+name, func(t *testing.T) {
			key, priv := freshDeviceKey(t)
			c, result := authTestConnection(t, key, time.Second)
			nonce := readDeviceChallenge(t, c)
			for i := 0; i < count; i++ {
				kind := websocket.PongMessage
				if i%2 == 0 {
					kind = websocket.PingMessage
				}
				_ = c.WriteControl(kind, nil, time.Now().Add(time.Second))
			}
			_ = c.WriteJSON(deviceProof(priv, key, nonce))
			err := authResult(t, result)
			if count == 8 && err != nil {
				t.Fatal(err)
			}
			if count == 9 && (err == nil || !strings.Contains(err.Error(), "control limit")) {
				t.Fatalf("expected control limit, got %v", err)
			}
		})
	}
}

func TestDeviceReportAuthenticationLifecycle(t *testing.T) {
	s, id := securityServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	key := reportTestPublicKey("security-token")
	sub := s.hub.SubscribeOverview()
	defer s.hub.UnsubscribeOverview(sub)
	first, _, err := dialReportTest(url, http.Header{"X-Agent-Public-Key": {key}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer s.cancelOfflineNotification(id)
	if !readStatus(t, sub) {
		t.Fatal("not online")
	}
	s.hub.mu.RLock()
	original := s.hub.agents[id]
	s.hub.mu.RUnlock()
	pending, _, err := websocket.DefaultDialer.Dial(url, http.Header{"X-Agent-Public-Key": {key}})
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	nonce := readDeviceChallenge(t, pending)
	s.hub.mu.RLock()
	unchanged := s.hub.agents[id] == original
	s.hub.mu.RUnlock()
	if !unchanged {
		t.Fatal("unauthenticated connection evicted original")
	}
	if err := first.WriteMessage(websocket.TextMessage, usageMessage(0)); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-sub.ch:
		var event struct{ Type string }
		_ = json.Unmarshal(raw, &event)
		if event.Type != "usage" {
			t.Fatalf("original cannot report: %s", raw)
		}
	case <-time.After(time.Second):
		t.Fatal("original report missing")
	}
	priv, _ := reportTestKeys.Load(key)
	err = s.hub.RevokeAgent(id, func() error {
		if err := pending.WriteJSON(deviceProof(priv.(ed25519.PrivateKey), key, nonce)); err != nil {
			return err
		}
		return s.store.DeleteHost(id)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pending.ReadMessage(); err == nil {
		t.Fatal("deleted device received auth_ok")
	}
	if s.hub.IsOnline(id) {
		t.Fatal("deleted device came online")
	}
}

func TestDeviceReportRejectsLegacyToken(t *testing.T) {
	s, _ := securityServer(t)
	for _, header := range []http.Header{{"Token": {"security-token"}}, {"Token": {"security-token"}, "X-Agent-Public-Key": {reportTestPublicKey("security-token")}}} {
		r := httptest.NewRequest(http.MethodGet, "/report", nil)
		r.Header = header
		w := httptest.NewRecorder()
		s.handleReport(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("legacy token status = %d", w.Code)
		}
	}
}

func TestDeviceReportPublicKeyOwnsUsage(t *testing.T) {
	s, otherID := securityServer(t)
	key, priv := freshDeviceKey(t)
	host, err := s.store.CreateHost("fresh", key, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"X-Agent-Public-Key": {key}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer s.cancelOfflineNotification(host.PublicID)
	nonce := readDeviceChallenge(t, c)
	if err := c.WriteJSON(deviceProof(priv, key, nonce)); err != nil {
		t.Fatal(err)
	}
	var ack struct{ Type string }
	if err := c.ReadJSON(&ack); err != nil || ack.Type != "auth_ok" {
		t.Fatalf("ack: %+v %v", ack, err)
	}
	sub := s.hub.SubscribeOverview()
	defer s.hub.UnsubscribeOverview(sub)
	if err := c.WriteJSON(map[string]any{"type": "system_usage", "publicId": otherID, "data": map[string]any{"timestamp": 0, "publicId": otherID}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case raw := <-sub.ch:
			var event struct{ Type, PublicID string }
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type != "usage" {
				continue
			}
			if event.PublicID != host.PublicID {
				t.Fatalf("wrong report owner: %s", raw)
			}
			own, err := s.store.LatestUsage(host.PublicID)
			if err != nil || own == nil {
				t.Fatalf("own usage: %v %v", own, err)
			}
			other, err := s.store.LatestUsage(otherID)
			if err != nil || other != nil {
				t.Fatalf("spoofed usage persisted: %v %v", other, err)
			}
			return
		case <-deadline:
			t.Fatal("usage not published")
		}
	}
}
