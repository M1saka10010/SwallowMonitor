package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
)

// handleReport handles agent WebSocket connections at /report.
// Authentication is via the "Token" request header which must match a
// registered host token.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	publicID, err := s.store.PublicIDByToken(token)
	if errors.Is(err, store.ErrNotFound) {
		s.debugf("report: rejected unknown token")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.debugf("report: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	if s.hub.AddAgent(publicID, conn) {
		s.hub.PublishOverview(map[string]any{"type": "status", "publicId": publicID, "online": true})
		go s.notifyHostStatus(publicID, true)
	}
	defer func() {
		if s.hub.RemoveAgent(publicID, conn) {
			s.hub.PublishOverview(map[string]any{"type": "status", "publicId": publicID, "online": false})
			go s.notifyHostStatus(publicID, false)
		}
	}()
	s.debugf("report: agent connected publicID=%s", publicID)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			s.debugf("report: read closed publicID=%s: %v", publicID, err)
			return
		}
		s.dispatch(publicID, raw)
	}
}

func (s *Server) dispatch(publicID string, raw []byte) {
	var env model.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		s.debugf("report: bad envelope: %v", err)
		return
	}

	switch env.Type {
	case model.TypeSystemInfo:
		var info model.SystemInfo
		if err := json.Unmarshal(env.Data, &info); err != nil {
			s.debugf("report: bad system_info: %v", err)
			return
		}
		if err := s.store.UpdateInfo(publicID, &info, string(env.Data)); err != nil {
			s.debugf("report: store info failed: %v", err)
		}

	case model.TypeSystemUsage:
		var usage model.SystemUsage
		if err := json.Unmarshal(env.Data, &usage); err != nil {
			s.debugf("report: bad system_usage: %v", err)
			return
		}
		if usage.Timestamp == 0 {
			usage.Timestamp = uint64(time.Now().Unix())
		}
		if err := s.store.InsertUsage(publicID, &usage); err != nil {
			s.debugf("report: store usage failed: %v", err)
		}
		_ = s.store.Touch(publicID, int64(usage.Timestamp))
		s.hub.PublishOverview(map[string]any{"type": "usage", "publicId": publicID, "data": usage})

	default:
		s.debugf("report: unknown type %q", env.Type)
	}
}
