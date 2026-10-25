package store

import (
	"encoding/json"
	"github.com/M1saka10010/SwallowMonitor/model"
	"path/filepath"
	"testing"
)

func TestAgentVersionPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "versions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	h, err := s.CreateHost("versioned", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	usage := &model.SystemUsage{Timestamp: 100, CPUUsage: 12.5}
	if err := s.InsertUsage(h.PublicID, usage); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, raw, want string }{
		{"legacy", `{"hostname":"old"}`, ""},
		{"development", `{"agentVersion":"dev"}`, "dev"},
		{"release", `{"agentVersion":"v1.2.3"}`, "v1.2.3"},
		{"upgrade", `{"agentVersion":"v2.0.0+build.7"}`, "v2.0.0+build.7"},
		{"missing after upgrade", `{"hostname":"older-agent"}`, ""},
		{"explicit empty", `{"agentVersion":""}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var info model.SystemInfo
			if err := json.Unmarshal([]byte(tc.raw), &info); err != nil {
				t.Fatal(err)
			}
			if info.AgentVersion != tc.want {
				t.Fatalf("decoded version = %q", info.AgentVersion)
			}
			if err := s.UpdateInfo(h.PublicID, &info, tc.raw); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.GetHost(h.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if got.AgentVersion != tc.want || got.LastInfoJSON != tc.raw {
				t.Fatalf("reopened host = %+v", got)
			}
			hosts, err := s.ListHosts()
			if err != nil {
				t.Fatal(err)
			}
			if len(hosts) != 1 || hosts[0].AgentVersion != tc.want {
				t.Fatalf("list = %+v", hosts)
			}
			history, err := s.QueryUsage(h.PublicID, 99, 101)
			if err != nil {
				t.Fatal(err)
			}
			if len(history) != 1 || *history[0] != *usage {
				t.Fatalf("history changed: %+v", history)
			}
		})
	}
}

func TestAgentVersionLegacySnapshots(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h, err := s.CreateHost("legacy", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []any{nil, "", "{}", "null", "not-json", `{"agentVersion":123}`} {
		if _, err := s.db.Exec("UPDATE hosts SET last_info_json=? WHERE public_id=?", raw, h.PublicID); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetHost(h.PublicID)
		if err != nil {
			t.Fatal(err)
		}
		if got.AgentVersion != "" {
			t.Fatalf("snapshot %v version = %q", raw, got.AgentVersion)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if result["agentVersion"] != "" {
			t.Fatalf("unknown version omitted or invented: %s", encoded)
		}
	}
}
