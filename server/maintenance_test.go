package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
)

func maintenanceRequest(s *Server, method, path, body string, login bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if login {
		r.AddCookie(&http.Cookie{Name: s.sessionCookieName(), Value: s.sessions.create("admin")})
	}
	w := httptest.NewRecorder()
	s.handleAPI(w, r)
	return w
}

func TestMaintenancePermissions(t *testing.T) {
	s, id := securityServer(t)
	s.oauth = securityAuthServer().oauth
	r := httptest.NewRequest("POST", "/api/hosts/"+id+"/maintenance", strings.NewReader(`{"kind":"system_update"}`))
	r.AddCookie(&http.Cookie{Name: s.sessionCookieName(), Value: s.sessions.create("admin")})
	r.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	s.handleAPI(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", w.Code)
	}
	for _, path := range []string{"/api/maintenance", "/api/maintenance/missing", "/api/hosts/" + id + "/maintenance"} {
		w := maintenanceRequest(s, "GET", path, "", false)
		if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %v", path, w.Code, w.Header())
		}
		w = maintenanceRequest(s, "DELETE", path, "", true)
		if w.Code != 405 {
			t.Fatalf("method accepted: %d", w.Code)
		}
	}
	path := "/api/hosts/" + id + "/maintenance"
	if w := maintenanceRequest(s, "POST", path, `{"kind":"system_update"}`, false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := maintenanceRequest(s, "POST", path, `{"kind":"system_update"}`, true); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	for _, body := range []string{`{"kind":"shell"}`, `{"kind":"system_update","command":"id"}`, `{"kind":"system_update","targetVersion":"v1.0.0"}`, `{"kind":"system_update"} {}`} {
		if w := maintenanceRequest(s, "POST", path, body, true); w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
}

func TestMaintenanceSyncConnectionAndReplay(t *testing.T) {
	s, id := securityServer(t)
	s.oauth = securityAuthServer().oauth
	if err := s.store.UpdateInfo(id, &model.SystemInfo{OS: "linux", KernelArch: "x86_64", AgentVersion: "v1.0.0"}, "{}"); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()
	dial := func() *websocket.Conn {
		c, _, err := dialReportTest("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"X-Agent-Public-Key": {reportTestPublicKey("security-token")}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := dial()
	defer c.Close()
	sync := func(events []store.MaintenanceEvent) struct {
		Acks []store.MaintenanceEvent
		Task *store.MaintenanceTask
	} {
		if err := c.WriteJSON(map[string]any{"type": "maintenance_sync", "data": map[string]any{"supported": true, "events": events}}); err != nil {
			t.Fatal(err)
		}
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		var reply struct {
			Type string
			Data struct {
				Acks []store.MaintenanceEvent
				Task *store.MaintenanceTask
			}
		}
		if err := c.ReadJSON(&reply); err != nil {
			t.Fatal(err)
		}
		if reply.Type != "maintenance_sync" {
			t.Fatal(reply.Type)
		}
		return reply.Data
	}
	path := "/api/hosts/" + id + "/maintenance"
	if w := maintenanceRequest(s, "POST", path, `{"kind":"system_update"}`, true); w.Code != 409 {
		t.Fatal("old agent", w.Code)
	}
	sync(nil)
	w := maintenanceRequest(s, "GET", path, "", true)
	var capability struct{ Supported, Online bool }
	if err := json.Unmarshal(w.Body.Bytes(), &capability); err != nil || !capability.Supported || !capability.Online {
		t.Fatal(w.Body)
	}
	w = maintenanceRequest(s, "POST", path, `{"kind":"system_update"}`, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	var task store.MaintenanceTask
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if w := maintenanceRequest(s, "POST", path, `{"kind":"system_update"}`, true); w.Code != 409 {
		t.Fatal("duplicate", w.Code)
	}
	for i := 0; i < 2; i++ {
		reply := sync(nil)
		if reply.Task == nil || reply.Task.ID != task.ID || reply.Task.Status != "delivered" {
			t.Fatalf("delivery: %+v", reply)
		}
	}
	other, err := s.store.CreateHost("other", reportTestPublicKey("other-maintenance"), nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := s.store.CreateMaintenance(other.PublicID, "system_update", "", "admin", time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	events := []store.MaintenanceEvent{{TaskID: foreign.ID, Seq: 1, Status: "accepted"}, {TaskID: task.ID, Seq: 1, Status: "accepted"}}
	for i := range events {
		events[i].CreatedAt = time.Now().Unix()
	}
	for i := 0; i < 2; i++ {
		reply := sync(events)
		if len(reply.Acks) != 1 || reply.Acks[0].TaskID != task.ID || reply.Task != nil {
			t.Fatalf("acks: %+v", reply)
		}
	}
	persisted, err := s.store.MaintenanceEvents(task.ID, 0, 200)
	if err != nil || len(persisted) != 1 {
		t.Fatal(persisted, err)
	}
	replacement := dial()
	defer replacement.Close()
	w = maintenanceRequest(s, "GET", path, "", true)
	if err := json.Unmarshal(w.Body.Bytes(), &capability); err != nil || capability.Supported || !capability.Online {
		t.Fatal("capability survived replacement", w.Body)
	}
}

func TestMaintenanceReplayAfterMonitorRestart(t *testing.T) {
	path := t.TempDir() + "/restart.db"
	key := reportTestPublicKey("maintenance-restart")
	var task *store.MaintenanceTask
	var publicID string
	now := time.Now().Unix()
	for phase := 0; phase < 2; phase++ {
		func() {
			st, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if phase == 0 {
				host, err := st.CreateHost("restart", key, nil)
				if err != nil {
					t.Fatal(err)
				}
				publicID = host.PublicID
				task, err = st.CreateMaintenance(publicID, "system_update", "", "admin", now)
				if err != nil {
					t.Fatal(err)
				}
			}
			s := New(&model.Config{}, st, http.NewServeMux(), http.NotFoundHandler())
			done := make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				s.handleReport(w, r)
			}))
			c, _, err := dialReportTest("ws"+strings.TrimPrefix(ts.URL, "http"), http.Header{"X-Agent-Public-Key": {key}})
			if err != nil {
				ts.Close()
				t.Fatal(err)
			}
			defer func() {
				c.Close()
				ts.Close()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("report handler did not stop")
				}
				s.cancelOfflineNotification(publicID)
			}()
			sync := func(events []store.MaintenanceEvent) (*store.MaintenanceTask, []store.MaintenanceEvent) {
				if err := c.WriteJSON(map[string]any{"type": "maintenance_sync", "data": map[string]any{"supported": true, "events": events}}); err != nil {
					t.Fatal(err)
				}
				c.SetReadDeadline(time.Now().Add(3 * time.Second))
				var reply struct {
					Type string
					Data struct {
						Task *store.MaintenanceTask
						Acks []store.MaintenanceEvent
					}
				}
				if err := c.ReadJSON(&reply); err != nil {
					t.Fatal(err)
				}
				if reply.Type != "maintenance_sync" {
					t.Fatal(reply.Type)
				}
				return reply.Data.Task, reply.Data.Acks
			}
			if phase == 0 {
				delivered, _ := sync(nil)
				if delivered == nil || delivered.ID != task.ID {
					t.Fatal("task not delivered")
				}
				_, acks := sync([]store.MaintenanceEvent{{TaskID: task.ID, Seq: 1, Status: "accepted", CreatedAt: now}})
				if len(acks) != 1 {
					t.Fatal("acceptance not persisted")
				}
				return
			}
			persisted, err := st.GetMaintenance(task.ID)
			if err != nil || persisted.LastSeq != 1 || persisted.Status != "accepted" {
				t.Fatalf("restart lost task: %+v %v", persisted, err)
			}
			events := []store.MaintenanceEvent{
				{TaskID: task.ID, Seq: 2, Status: "running", Message: "offline package update", CreatedAt: now},
				{TaskID: task.ID, Seq: 3, Status: "verifying", Message: "offline verification log", CreatedAt: now},
				{TaskID: task.ID, Seq: 4, Status: "succeeded", Message: "offline completion", CreatedAt: now},
			}
			for replay := 0; replay < 2; replay++ {
				delivered, acks := sync(events)
				if delivered != nil || len(acks) != len(events) {
					t.Fatalf("replay response: %+v %+v", delivered, acks)
				}
				for i, ack := range acks {
					if ack.TaskID != task.ID || ack.Seq != events[i].Seq {
						t.Fatalf("wrong ack: %+v", ack)
					}
				}
			}
			persisted, err = st.GetMaintenance(task.ID)
			if err != nil || persisted.Status != "succeeded" || persisted.LastSeq != 4 {
				t.Fatalf("final task: %+v %v", persisted, err)
			}
			logs, err := st.MaintenanceEvents(task.ID, 0, 200)
			if err != nil || len(logs) != 4 {
				t.Fatalf("duplicate or missing logs: %+v %v", logs, err)
			}
			for i, event := range events {
				if logs[i+1] != event {
					t.Fatalf("log changed: %+v", logs[i+1])
				}
			}
		}()
	}
}

func TestMaintenanceUpgradeValidation(t *testing.T) {
	h := &store.Host{AgentVersion: "v1.0.0", KernelArch: "x86_64"}
	assets := []string{"bootstrap.sh", "upgrade.sh", "checksums.txt", "swallow-agent-linux-amd64"}
	releases := []agentRelease{{Version: "v2.0.0", Assets: assets}}
	if !validMaintenanceUpgrade(h, "v2.0.0", releases) {
		t.Fatal("valid upgrade rejected")
	}
	for _, target := range []string{"v1.0.0", "v0.9.0", "v2.0.0-rc1", "v3.0.0", "bad"} {
		if validMaintenanceUpgrade(h, target, releases) {
			t.Fatal(target)
		}
	}
	for i := range assets {
		missing := append([]string{}, assets[:i]...)
		missing = append(missing, assets[i+1:]...)
		if validMaintenanceUpgrade(h, "v2.0.0", []agentRelease{{Version: "v2.0.0", Assets: missing}}) {
			t.Fatal("missing asset", assets[i])
		}
	}
	h.KernelArch = "aarch64"
	if validMaintenanceUpgrade(h, "v2.0.0", releases) {
		t.Fatal("wrong architecture")
	}
	h.AgentVersion = "dev"
	if validMaintenanceUpgrade(h, "v2.0.0", releases) {
		t.Fatal("unknown current version")
	}
}
