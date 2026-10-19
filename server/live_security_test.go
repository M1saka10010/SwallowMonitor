package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type eventSecurityWriter struct {
	header    http.Header
	fail      string
	deadlines []time.Time
}

func (w *eventSecurityWriter) Header() http.Header { return w.header }
func (w *eventSecurityWriter) WriteHeader(int)     {}
func (w *eventSecurityWriter) Write(p []byte) (int, error) {
	if w.fail == "write" {
		return 0, errors.New("write")
	}
	return len(p), nil
}
func (w *eventSecurityWriter) FlushError() error {
	if w.fail == "flush" {
		return errors.New("flush")
	}
	return nil
}
func (w *eventSecurityWriter) SetWriteDeadline(d time.Time) error {
	w.deadlines = append(w.deadlines, d)
	if w.fail == "deadline" {
		return errors.New("deadline")
	}
	return nil
}

func TestSecurityEventWrites(t *testing.T) {
	for _, fail := range []string{"", "write", "flush", "deadline"} {
		w := &eventSecurityWriter{header: make(http.Header), fail: fail}
		err := writeEvent(w, http.NewResponseController(w), "test")
		if (err != nil) != (fail != "") {
			t.Fatalf("%s: %v", fail, err)
		}
		if len(w.deadlines) == 0 || w.deadlines[0].IsZero() {
			t.Fatal("no write deadline")
		}
		if fail == "" && (len(w.deadlines) != 2 || !w.deadlines[1].IsZero()) {
			t.Fatal("deadline not reset")
		}
		if fail != "" {
			s := &Server{hub: NewHub()}
			s.handleEvents(w, httptest.NewRequest("GET", "/events", nil))
			if s.hub.eventCount != 0 || len(s.hub.overview) != 0 {
				t.Fatal("failed stream leaked resources")
			}
		}
	}
}

func TestSecurityEventBudgets(t *testing.T) {
	h := NewHub()
	for i := 0; i < maxEventConnectionsPerIP; i++ {
		if !h.acquireEvents("one") {
			t.Fatal("early rejection")
		}
	}
	if h.acquireEvents("one") {
		t.Fatal("per-IP limit bypassed")
	}
	for i := maxEventConnectionsPerIP; i < maxEventConnections; i++ {
		if !h.acquireEvents(fmt.Sprint(i)) {
			t.Fatal("early global rejection")
		}
	}
	if h.acquireEvents("another") {
		t.Fatal("global limit bypassed")
	}
	h.releaseEvents("one")
	if !h.acquireEvents("another") {
		t.Fatal("budget not released")
	}
	s := &Server{hub: h}
	w := httptest.NewRecorder()
	s.handleEvents(w, httptest.NewRequest("GET", "/events", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d", w.Code)
	}
}
