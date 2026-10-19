package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
)

const (
	maxReportMessageSize    = 1 << 20
	reportReadTimeout       = 2 * time.Minute
	reportMessagesPerMinute = 120
	maxUsageClockSkew       = 5 * time.Minute
)

// handleReport handles agent WebSocket connections at /report.
// Authentication is via the "Token" request header which must match a
// registered host token.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	// Rate-limit by source IP before any DB access to prevent token
	// brute-force and DB-flooding DoS.
	if !s.reportLimiter.Allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

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
	conn.SetReadLimit(maxReportMessageSize)
	if err := conn.SetReadDeadline(time.Now().Add(reportReadTimeout)); err != nil {
		return
	}
	defaultPingHandler := conn.PingHandler()
	defaultCloseHandler := conn.CloseHandler()
	consumeBudget := func() error {
		if s.hub.messageLimiter.Allow(publicID) {
			return nil
		}
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "message rate exceeded"), time.Now().Add(time.Second))
		return errors.New("message rate exceeded")
	}
	conn.SetPingHandler(func(message string) error {
		if err := consumeBudget(); err != nil {
			return err
		}
		return defaultPingHandler(message)
	})
	conn.SetPongHandler(func(string) error {
		if err := consumeBudget(); err != nil {
			return err
		}
		return conn.SetReadDeadline(time.Now().Add(reportReadTimeout))
	})
	conn.SetCloseHandler(func(code int, text string) error {
		if err := consumeBudget(); err != nil {
			return err
		}
		return defaultCloseHandler(code, text)
	})

	// Recheck under the same lock used for deletion: the upgrade may have
	// overlapped revocation after the initial token lookup.
	s.hub.lifecycle.Lock()
	currentID, err := s.store.PublicIDByToken(token)
	if err != nil || currentID != publicID {
		s.hub.lifecycle.Unlock()
		return
	}
	becameOnline := s.hub.AddAgent(publicID, conn)
	if becameOnline {
		s.hub.PublishOverview(map[string]any{"type": "status", "publicId": publicID, "online": true})
		if s.cancelOfflineNotification(publicID) {
			s.debugf("report: suppressed online notification publicID=%s", publicID)
		} else {
			go s.notifyHostStatus(publicID, true)
		}
	}
	s.hub.lifecycle.Unlock()
	defer func() {
		s.hub.lifecycle.Lock()
		defer s.hub.lifecycle.Unlock()
		if s.hub.RemoveAgent(publicID, conn) {
			s.hub.PublishOverview(map[string]any{"type": "status", "publicId": publicID, "online": false})
			s.scheduleOfflineNotification(publicID)
		}
	}()
	s.debugf("report: agent connected publicID=%s", publicID)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			s.debugf("report: read closed publicID=%s: %v", publicID, err)
			return
		}
		if err := consumeBudget(); err != nil {
			return
		}
		if err := conn.SetReadDeadline(time.Now().Add(reportReadTimeout)); err != nil {
			return
		}
		s.hub.lifecycle.RLock()
		s.hub.mu.RLock()
		current := s.hub.agents[publicID] == conn
		s.hub.mu.RUnlock()
		if current {
			s.dispatch(publicID, raw)
		}
		s.hub.lifecycle.RUnlock()
		if !current {
			return
		}
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
		now := time.Now().Unix()
		if usage.Timestamp == 0 {
			usage.Timestamp = uint64(now)
		}
		skew := int64(maxUsageClockSkew / time.Second)
		if usage.Timestamp < uint64(now-skew) || usage.Timestamp > uint64(now+skew) {
			s.debugf("report: rejected usage timestamp publicID=%s", publicID)
			return
		}
		if err := s.store.InsertUsage(publicID, &usage); err != nil {
			s.debugf("report: store usage failed: %v", err)
			return
		}
		_ = s.store.Touch(publicID, now)
		s.hub.PublishOverview(map[string]any{"type": "usage", "publicId": publicID, "data": usage})

	default:
		s.debugf("report: unknown type %q", env.Type)
	}
}
