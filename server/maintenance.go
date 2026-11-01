package server

import (
	"encoding/json"
	"errors"
	"github.com/M1saka10010/SwallowMonitor/store"
	"io"
	"net/http"
	"strconv"
	"time"
)

type maintenanceCreate struct {
	Kind          string `json:"kind"`
	TargetVersion string `json:"targetVersion"`
}

func (s *Server) apiListMaintenance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireUser(w, r) {
		return
	}
	if err := s.store.ExpireMaintenance(); err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	before, err := maintenanceCursor(r.URL.Query().Get("before"))
	if err != nil {
		http.Error(w, "invalid cursor", 400)
		return
	}
	items, e := s.store.ListMaintenance(r.URL.Query().Get("publicId"), before, 200)
	if e != nil {
		http.Error(w, "internal error", 500)
		return
	}
	more := len(items) > 200
	if more {
		items = items[:200]
	}
	out := map[string]any{"tasks": items}
	if more {
		out["nextBefore"] = items[len(items)-1].Cursor
	}
	writeJSON(w, 200, out)
}
func (s *Server) apiHostMaintenance(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireUser(w, r) {
		return
	}
	_, e := s.store.GetHost(id)
	if errors.Is(e, store.ErrNotFound) {
		http.Error(w, "not found", 404)
		return
	}
	if e != nil {
		http.Error(w, "internal error", 500)
		return
	}
	if err := s.store.ExpireMaintenance(); err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	before, err := maintenanceCursor(r.URL.Query().Get("before"))
	if err != nil {
		http.Error(w, "invalid cursor", 400)
		return
	}
	items, e := s.store.ListMaintenance(id, before, 50)
	if e != nil {
		http.Error(w, "internal error", 500)
		return
	}
	s.hub.mu.RLock()
	online := s.hub.agents[id] != nil
	supported := online && s.hub.maintenanceSupported[id]
	s.hub.mu.RUnlock()
	out := map[string]any{"supported": supported, "online": online, "tasks": items}
	if len(items) > 50 {
		items = items[:50]
		out["tasks"] = items
		out["nextBefore"] = items[len(items)-1].Cursor
	}
	writeJSON(w, 200, out)
}
func (s *Server) apiCreateMaintenance(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireUser(w, r) {
		return
	}
	var b maintenanceCreate
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&b) != nil || dec.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid json", 400)
		return
	}
	if b.Kind != "agent_upgrade" && b.Kind != "system_update" {
		http.Error(w, "invalid kind", 400)
		return
	}
	if b.Kind == "system_update" && b.TargetVersion != "" {
		http.Error(w, "system update takes no target", 400)
		return
	}
	var releases agentReleaseResponse
	if b.Kind == "agent_upgrade" {
		releases = s.agentReleases.get()
	}
	by, _ := s.currentUser(r)
	if by == "" {
		by = "development"
	}
	task, status, message := func() (*store.MaintenanceTask, int, string) {
		s.hub.lifecycle.RLock()
		defer s.hub.lifecycle.RUnlock()
		s.hub.mu.RLock()
		defer s.hub.mu.RUnlock()
		if s.hub.agents[id] == nil || !s.hub.maintenanceSupported[id] {
			return nil, 409, "host offline or maintenance unsupported"
		}
		h, err := s.store.GetHost(id)
		if errors.Is(err, store.ErrNotFound) {
			return nil, 404, "not found"
		}
		if err != nil {
			return nil, 500, "internal error"
		}
		if h.OS != "linux" {
			return nil, 409, "Linux required"
		}
		if b.Kind == "agent_upgrade" {
			if releases.Stale || releases.CheckedAt == 0 || time.Now().Unix()-releases.CheckedAt > int64(agentReleaseTTL/time.Second) {
				return nil, 409, "release unavailable"
			}
			if !validMaintenanceUpgrade(h, b.TargetVersion, releases.Releases) {
				return nil, 400, "invalid target version or incomplete release"
			}
		}
		if err := s.store.ExpireMaintenance(); err != nil {
			return nil, 500, "internal error"
		}
		task, err := s.store.CreateMaintenance(id, b.Kind, b.TargetVersion, by, time.Now().Unix())
		if errors.Is(err, store.ErrMaintenanceConflict) {
			return nil, 409, "maintenance already active"
		}
		if err != nil {
			return nil, 500, "internal error"
		}
		return task, 201, ""
	}()
	if status != 201 {
		http.Error(w, message, status)
		return
	}
	writeJSON(w, 201, task)
}

func validMaintenanceUpgrade(h *store.Host, target string, releases []agentRelease) bool {
	if len(target) > 64 || !stableAgentTag.MatchString(target) || !stableAgentTag.MatchString(h.AgentVersion) || !agentVersionGreater(target, h.AgentVersion) {
		return false
	}
	arch := ""
	switch h.KernelArch {
	case "x86_64", "amd64":
		arch = "amd64"
	case "aarch64", "arm64":
		arch = "arm64"
	default:
		return false
	}
	for _, release := range releases {
		if release.Version != target {
			continue
		}
		assets := make(map[string]bool)
		for _, asset := range release.Assets {
			assets[asset] = true
		}
		return assets["bootstrap.sh"] && assets["upgrade.sh"] && assets["checksums.txt"] && assets["swallow-agent-linux-"+arch]
	}
	return false
}

func (s *Server) apiMaintenanceDetail(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireUser(w, r) {
		return
	}
	t, e := s.store.GetMaintenance(id)
	if errors.Is(e, store.ErrNotFound) {
		http.Error(w, "not found", 404)
		return
	}
	if e != nil {
		http.Error(w, "internal error", 500)
		return
	}
	after, err := maintenanceCursor(r.URL.Query().Get("after"))
	if err != nil {
		http.Error(w, "invalid cursor", 400)
		return
	}
	ev, e := s.store.MaintenanceEvents(id, after, 200)
	if e != nil {
		http.Error(w, "internal error", 500)
		return
	}
	more := len(ev) > 200
	if more {
		ev = ev[:200]
	}
	next := after
	if len(ev) > 0 {
		next = ev[len(ev)-1].Seq
	}
	writeJSON(w, 200, map[string]any{"task": t, "events": ev, "nextAfter": next, "hasMore": more})
}

func maintenanceCursor(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, errors.New("invalid cursor")
	}
	return value, nil
}
