package server

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	maxEventConnections      = 256
	maxEventConnectionsPerIP = 8
	eventWriteTimeout        = 10 * time.Second
)

func writeEvent(w http.ResponseWriter, rc *http.ResponseController, data string) error {
	if err := rc.SetWriteDeadline(time.Now().Add(eventWriteTimeout)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, data); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return err
	}
	return rc.SetWriteDeadline(time.Time{})
}

// handleEvents streams overview events (usage + online status of all hosts) to
// the browser via Server-Sent Events. Used by both the overview and detail
// pages (the detail page filters by host client-side).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if !s.hub.acquireEvents(ip) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "too many event streams", http.StatusTooManyRequests)
		return
	}
	defer s.hub.releaseEvents(ip)
	rc := http.NewResponseController(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := s.hub.SubscribeOverview()
	defer s.hub.UnsubscribeOverview(sub)

	// Retry hint for EventSource reconnects.
	if err := writeEvent(w, rc, "retry: 3000\n\n"); err != nil {
		return
	}

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-sub.ch:
			if !ok {
				return
			}
			if err := writeEvent(w, rc, fmt.Sprintf("data: %s\n\n", data)); err != nil {
				return
			}
		case <-ping.C:
			if err := writeEvent(w, rc, ": ping\n\n"); err != nil {
				return
			}
		}
	}
}
