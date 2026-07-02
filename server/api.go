package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/M1saka10010/SwallowMonitor/store"
)

// hostView is the API representation of a host with derived fields.
type hostView struct {
	*store.Host
	Online bool `json:"online"`
	Latest any  `json:"latest,omitempty"`
}

// handleAPI routes /api/* requests.
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch {
	case len(parts) == 1 && parts[0] == "me":
		s.apiMe(w, r)

	case len(parts) == 1 && parts[0] == "settings":
		switch r.Method {
		case http.MethodGet:
			s.apiGetSettings(w, r)
		case http.MethodPatch:
			s.apiUpdateSettings(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) == 1 && parts[0] == "notifications":
		switch r.Method {
		case http.MethodGet:
			s.apiListNotifications(w, r)
		case http.MethodPost:
			s.apiCreateNotification(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) == 2 && parts[0] == "notifications":
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			s.apiUpdateNotification(w, r, id)
		case http.MethodDelete:
			s.apiDeleteNotification(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) == 1 && parts[0] == "tags":
		switch r.Method {
		case http.MethodGet:
			s.apiListTags(w, r)
		case http.MethodPost:
			s.apiCreateTag(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) == 2 && parts[0] == "tags":
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			s.apiUpdateTag(w, r, id)
		case http.MethodDelete:
			s.apiDeleteTag(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) == 1 && parts[0] == "hosts":
		switch r.Method {
		case http.MethodGet:
			s.apiListHosts(w, r)
		case http.MethodPost:
			s.apiCreateHost(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) == 2 && parts[0] == "hosts":
		publicID := parts[1]
		switch r.Method {
		case http.MethodGet:
			s.apiGetHost(w, r, publicID)
		case http.MethodPatch:
			s.apiUpdateHost(w, r, publicID)
		case http.MethodDelete:
			s.apiDeleteHost(w, r, publicID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) == 3 && parts[0] == "hosts" && parts[2] == "usage":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.apiUsage(w, r, parts[1])

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// apiMe reports the current authentication state for the web client.
func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"authEnabled": s.oauth != nil, "user": "", "loggedIn": false}
	if s.oauth == nil {
		// No OAuth configured: management is open to everyone.
		resp["loggedIn"] = true
	} else if user, ok := s.currentUser(r); ok {
		resp["loggedIn"] = true
		resp["user"] = user
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) apiGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetSiteSettings()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) apiUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireUser(w, r) {
		return
	}
	var body store.SiteSettings
	if err := decodeJSON(r.Body, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	body.SiteName = strings.TrimSpace(body.SiteName)
	body.SiteDescription = strings.TrimSpace(body.SiteDescription)
	if body.SiteName == "" {
		http.Error(w, "siteName required", http.StatusBadRequest)
		return
	}
	if len([]rune(body.SiteName)) > 60 {
		http.Error(w, "siteName too long", http.StatusBadRequest)
		return
	}
	if len([]rune(body.SiteDescription)) > 200 {
		http.Error(w, "siteDescription too long", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateSiteSettings(body); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) apiListNotifications(w http.ResponseWriter, r *http.Request) {
	if !s.requireUser(w, r) {
		return
	}
	rules, err := s.store.ListNotificationRules()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) apiCreateNotification(w http.ResponseWriter, r *http.Request) {
	if !s.requireUser(w, r) {
		return
	}
	var body store.NotificationRule
	if err := decodeJSON(r.Body, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	rule, msg := normalizeNotificationRule(body)
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	created, err := s.store.CreateNotificationRule(rule)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) apiUpdateNotification(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.requireUser(w, r) {
		return
	}
	var body store.NotificationRule
	if err := decodeJSON(r.Body, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	body.ID = id
	rule, msg := normalizeNotificationRule(body)
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateNotificationRule(rule); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	updated, err := s.store.GetNotificationRule(id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) apiDeleteNotification(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.requireUser(w, r) {
		return
	}
	if err := s.store.DeleteNotificationRule(id); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) apiListTags(w http.ResponseWriter, r *http.Request) {
	if !s.requireUser(w, r) {
		return
	}
	tags, err := s.store.ListTags()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) apiCreateTag(w http.ResponseWriter, r *http.Request) {
	if !s.requireUser(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r.Body, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if len([]rune(name)) > 40 {
		http.Error(w, "name too long", http.StatusBadRequest)
		return
	}
	tag, err := s.store.CreateTag(name)
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, tag)
}

func (s *Server) apiUpdateTag(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.requireUser(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r.Body, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if len([]rune(name)) > 40 {
		http.Error(w, "name too long", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateTag(id, name); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "update failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	tag, err := s.store.GetTag(id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func (s *Server) apiDeleteTag(w http.ResponseWriter, r *http.Request, id int64) {
	if !s.requireUser(w, r) {
		return
	}
	if err := s.store.DeleteTag(id); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) apiListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.store.ListHosts()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	canManage := s.canManage(r)
	now := time.Now().Unix()
	views := make([]hostView, 0, len(hosts))
	for _, h := range hosts {
		if !canManage {
			h.Token = "" // never expose tokens to anonymous viewers
		}
		latest, _ := s.store.LatestUsage(h.PublicID)
		views = append(views, hostView{
			Host:   h,
			Online: s.isOnline(h, now),
			Latest: latest,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) apiGetHost(w http.ResponseWriter, r *http.Request, publicID string) {
	h, err := s.store.GetHost(publicID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !s.canManage(r) {
		h.Token = ""
	}
	latest, _ := s.store.LatestUsage(publicID)
	writeJSON(w, http.StatusOK, hostView{
		Host:   h,
		Online: s.isOnline(h, time.Now().Unix()),
		Latest: latest,
	})
}

func (s *Server) apiCreateHost(w http.ResponseWriter, r *http.Request) {
	if !s.requireUser(w, r) {
		return
	}
	var body struct {
		Nickname string   `json:"nickname"`
		Token    string   `json:"token"`
		Tags     []string `json:"tags"`
	}
	if err := decodeJSON(r.Body, &body); err != nil || strings.TrimSpace(body.Nickname) == "" {
		http.Error(w, "nickname required", http.StatusBadRequest)
		return
	}
	h, err := s.store.CreateHost(strings.TrimSpace(body.Nickname), strings.TrimSpace(body.Token), cleanTags(body.Tags))
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Token is returned exactly once on creation.
	writeJSON(w, http.StatusCreated, h)
}

func (s *Server) apiUpdateHost(w http.ResponseWriter, r *http.Request, publicID string) {
	if !s.requireUser(w, r) {
		return
	}
	var body struct {
		Nickname string   `json:"nickname"`
		Tags     []string `json:"tags"`
	}
	if err := decodeJSON(r.Body, &body); err != nil || strings.TrimSpace(body.Nickname) == "" {
		http.Error(w, "nickname required", http.StatusBadRequest)
		return
	}
	err := s.store.UpdateHost(publicID, strings.TrimSpace(body.Nickname), cleanTags(body.Tags))
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) apiDeleteHost(w http.ResponseWriter, r *http.Request, publicID string) {
	if !s.requireUser(w, r) {
		return
	}
	err := s.store.DeleteHost(publicID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) apiUsage(w http.ResponseWriter, r *http.Request, publicID string) {
	q := r.URL.Query()
	now := time.Now().Unix()
	from := parseInt(q.Get("from"), now-3600)
	to := parseInt(q.Get("to"), now)

	// Clamp the requested span to a sane maximum (matches the largest UI range)
	// to prevent oversized scans from hand-crafted requests.
	if to < from {
		from, to = to, from
	}
	if to-from > maxUsageSpan {
		from = to - maxUsageSpan
	}

	points, err := s.store.QueryUsage(publicID, from, to)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

// maxUsageSpan caps the usage query window to 7 days.
const maxUsageSpan int64 = 7 * 24 * 3600

func (s *Server) isOnline(h *store.Host, now int64) bool {
	if s.hub.IsOnline(h.PublicID) {
		return true
	}
	return h.LastSeen > 0 && now-h.LastSeen < s.cfg.OfflineTimeout
}

func parseInt(s string, def int64) int64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return v
}

// cleanTags trims, drops empties, and de-duplicates tags.
func cleanTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
