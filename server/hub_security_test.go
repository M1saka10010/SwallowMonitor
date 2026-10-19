package server

import (
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSecurityRevokeAgent(t *testing.T) {
	s, id := securityServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deadline := time.Now().Add(time.Second)
	for !s.hub.IsOnline(id) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.hub.IsOnline(id) {
		t.Fatal("not registered")
	}
	rejected := errors.New("delete failed")
	if err := s.hub.RevokeAgent(id, func() error { return rejected }); !errors.Is(err, rejected) || !s.hub.IsOnline(id) {
		t.Fatal("failed deletion removed connection")
	}
	if err := s.hub.RevokeAgent(id, func() error { return s.store.DeleteHost(id) }); err != nil {
		t.Fatal(err)
	}
	if s.hub.IsOnline(id) {
		t.Fatal("revoked host online")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("revoked connection open")
	}
	_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
	if err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("revoked token accepted")
	}
}

func TestSecurityRevokeDuringHandshake(t *testing.T) {
	s, id := securityServer(t)
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()
	entered, proceed, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- s.hub.RevokeAgent(id, func() error { close(entered); <-proceed; return s.store.DeleteHost(id) })
	}()
	<-entered
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"Token": {"security-token"}})
	close(proceed)
	if deleteErr := <-done; deleteErr != nil {
		t.Fatal(deleteErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("handshake survived revocation")
	}
	if s.hub.IsOnline(id) {
		t.Fatal("deleted host registered")
	}
}
