package server

import (
	"encoding/json"
	"github.com/M1saka10010/SwallowMonitor/buildinfo"
	"github.com/M1saka10010/SwallowMonitor/model"
	"net/http/httptest"
	"testing"
)

func TestMonitorVersion(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		s := &Server{cfg: &model.Config{InsecureDevelopment: allowed}, sessions: newSessionStore()}
		w := httptest.NewRecorder()
		s.apiMe(w, httptest.NewRequest("GET", "/api/me", nil))
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if allowed && body["monitorVersion"] != buildinfo.Version {
			t.Fatalf("version = %v", body["monitorVersion"])
		}
		if !allowed && body["monitorVersion"] != nil {
			t.Fatal("version exposed without management access")
		}
	}
}
