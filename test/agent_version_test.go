package test

import (
	"encoding/json"
	"fmt"
	"github.com/M1saka10010/SwallowMonitor/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAgentVersionReportToHostAPI(t *testing.T) {
	st, mux := newTestApp(t, nil)
	host, err := st.CreateHost("versioned", reportTestPublicKey("agent-version"), nil)
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(mux)
	defer app.Close()
	conn, _, err := dialReportTest("ws"+strings.TrimPrefix(app.URL, "http")+"/report", http.Header{"X-Agent-Public-Key": []string{host.PublicKey}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	now := time.Now().Unix()
	usage := model.SystemUsage{Timestamp: uint64(now), CPUUsage: 42}
	if err := conn.WriteJSON(map[string]any{"type": model.TypeSystemUsage, "data": usage}); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ raw, want string }{
		{`{}`, ""},
		{`{"agentVersion":"dev"}`, "dev"},
		{`{"agentVersion":"v1.2.3"}`, "v1.2.3"},
		{`{"agentVersion":"v2.0.0"}`, "v2.0.0"},
		{`{}`, ""},
		{`{"agentVersion":""}`, ""},
	} {
		var data map[string]any
		if err := json.Unmarshal([]byte(tc.raw), &data); err != nil {
			t.Fatal(err)
		}
		hostname := fmt.Sprintf("report-%d", i)
		data["hostname"] = hostname
		if err := conn.WriteJSON(map[string]any{"type": model.TypeSystemInfo, "data": data}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			got, err := st.GetHost(host.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Hostname == hostname {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("report not ingested")
			}
			time.Sleep(10 * time.Millisecond)
		}
		for _, path := range []string{"/api/hosts", "/api/hosts/" + host.PublicID} {
			resp, err := app.Client().Get(app.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			var body json.RawMessage
			err = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
			}
			var got map[string]any
			if path == "/api/hosts" {
				var list []map[string]any
				if err := json.Unmarshal(body, &list); err != nil {
					t.Fatal(err)
				}
				if len(list) != 1 {
					t.Fatalf("list = %s", body)
				}
				got = list[0]
			} else if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			if got["agentVersion"] != tc.want {
				t.Fatalf("%s version = %#v, want %q", path, got["agentVersion"], tc.want)
			}
		}
		history, err := st.QueryUsage(host.PublicID, now-1, now+1)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 1 || *history[0] != usage {
			t.Fatalf("history changed: %+v", history)
		}
	}
}
