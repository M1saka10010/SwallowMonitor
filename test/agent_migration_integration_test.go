//go:build agentintegration && linux

package test

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/server"
	"github.com/M1saka10010/SwallowMonitor/store"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentTokenMigrationIntegration(t *testing.T) {
	binary := os.Getenv("SWALLOW_AGENT_BINARY")
	if binary == "" {
		repo := os.Getenv("SWALLOW_AGENT_REPO")
		if repo == "" {
			t.Fatal("set SWALLOW_AGENT_REPO or SWALLOW_AGENT_BINARY")
		}
		binary = filepath.Join(t.TempDir(), "agent")
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "monitor.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`DROP TABLE hosts; CREATE TABLE hosts (
 public_id TEXT PRIMARY KEY, token TEXT NOT NULL UNIQUE, nickname TEXT NOT NULL, tags TEXT,
 host_id TEXT, hostname TEXT, os TEXT, platform TEXT, platform_version TEXT,
 kernel_arch TEXT, model_name TEXT, cores INTEGER, virtualization_role TEXT,
 boot_time INTEGER, last_info_json TEXT, last_seen INTEGER, created_at INTEGER);
 INSERT INTO hosts VALUES ('historical','old-token','old-name','[]','machine','old-host','linux','platform','1','amd64','cpu',4,'guest',123,'{}',456,100);
 INSERT INTO usages(public_id,ts,cpu_usage) VALUES ('historical',456,12);
 INSERT INTO usages_5m(public_id,bucket_ts,cpu_usage) VALUES ('historical',300,13);
 INSERT INTO usages_1h(public_id,bucket_ts,cpu_usage) VALUES ('historical',0,14);`)
	if err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	enrollment, err := st.RefreshEnrollment("historical")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.New(&model.Config{}, st, mux, http.NotFoundHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()
	reportURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/report"
	old, config, key := filepath.Join(dir, "old.yml"), filepath.Join(dir, "new.yml"), filepath.Join(dir, "agent.key")
	if err := os.WriteFile(old, []byte(fmt.Sprintf("paths: ['/']\ninterfaceNames: []\ninterval: 1\nreportUrl: %s\ntoken: old-token\ndevMode: true\n", reportURL)), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(token string, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Stdin = strings.NewReader(token)
		return cmd.CombinedOutput()
	}
	if out, err := run("", "-prepare-token-migration", old, "-output-config", config, "-default-private-key", key, "-enrollment-token-stdin"); err != nil {
		t.Fatalf("prepare: %v %s", err, out)
	}
	for attempt := 0; attempt < 2; attempt++ {
		token := enrollment.Token
		if attempt == 1 {
			token = ""
		}
		if out, err := run(token, "-enroll-token-migration", config, "-enrollment-token-stdin"); err != nil {
			t.Fatalf("migration %d: %v %s", attempt, err, out)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var reports int
		if err := db.QueryRow("SELECT count(*) FROM usages WHERE public_id='historical' AND ts>456").Scan(&reports); err != nil {
			t.Fatal(err)
		}
		if reports >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real usage reports not persisted: %d", reports)
		}
		time.Sleep(20 * time.Millisecond)
	}
	host, err := st.GetHost("historical")
	if err != nil || host.PublicKey == "" || host.Nickname != "old-name" || host.LastSeen <= 456 {
		t.Fatalf("host identity/metadata lost: %+v %v", host, err)
	}
	var count, created int
	var info string
	if err := db.QueryRow("SELECT count(*) FROM hosts").Scan(&count); err != nil || count != 1 {
		t.Fatalf("new host created: %d %v", count, err)
	}
	if err := db.QueryRow("SELECT created_at,last_info_json FROM hosts WHERE public_id='historical'").Scan(&created, &info); err != nil || created != 100 || info == "{}" {
		t.Fatalf("system info not persisted or creation changed: %d %s %v", created, info, err)
	}
	for i, table := range []string{"usages", "usages_5m", "usages_1h"} {
		var value float64
		if err := db.QueryRow("SELECT cpu_usage FROM "+table+" WHERE public_id='historical' AND cpu_usage=?", 12+i).Scan(&value); err != nil || value != float64(12+i) {
			t.Fatalf("lost %s history: %v %v", table, value, err)
		}
	}
	bad := filepath.Join(dir, "bad.yml")
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "- /", "- /nonexistent-swallow-migration-test-path", 1))
	if !strings.Contains(string(data), "nonexistent-swallow") {
		t.Fatal("missing paths fixture")
	}
	if err := os.WriteFile(bad, data, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := run("", "-enroll-token-migration", bad, "-enrollment-token-stdin"); err == nil || !strings.Contains(string(out), "migration collection failed") {
		t.Fatalf("collector failure accepted: %v %s", err, out)
	}
}
