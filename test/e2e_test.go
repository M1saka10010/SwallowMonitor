//go:build e2e

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
)

// e2eServer wraps a running swallow-monitor process.
type e2eServer struct {
	cmd    *exec.Cmd
	addr   string
	tmpDir string
}

// startE2EServer builds the binary (if needed), writes a temp config, and
// starts the server. The caller must call shutdown() to clean up.
func startE2EServer(t *testing.T) *e2eServer {
	t.Helper()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "swallow-monitor")

	// Build the binary from the project root.
	build := exec.Command("go", "build", "-trimpath", "-o", binPath, ".")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	dbPath := filepath.Join(tmpDir, "swallow.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfg := fmt.Sprintf("listen: %q\ndbPath: %q\nofflineTimeout: 90\n", addr, dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(binPath, "-c", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}

	s := &e2eServer{cmd: cmd, addr: addr, tmpDir: tmpDir}
	t.Cleanup(func() { s.shutdown(t) })

	// Wait until the server is ready to accept connections.
	baseURL := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/me")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.shutdown(t)
	t.Fatal("server did not become ready within 10s")
	return nil
}

func (s *e2eServer) shutdown(t *testing.T) {
	t.Helper()
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Logf("SIGTERM: %v", err)
		return
	}
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		s.cmd.Process.Kill()
		<-done
		t.Log("server did not shut down gracefully, killed")
	}
}

func (s *e2eServer) url(path string) string {
	return "http://" + s.addr + path
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestE2EServerStartsAndResponds(t *testing.T) {
	s := startE2EServer(t)

	resp, err := http.Get(s.url("/api/me"))
	if err != nil {
		t.Fatalf("GET /api/me: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/me: %v", err)
	}
	if body["authEnabled"] != false || body["loggedIn"] != true {
		t.Fatalf("/api/me = %#v, want authEnabled=false, loggedIn=true", body)
	}
}

func TestE2EHostCRUD(t *testing.T) {
	s := startE2EServer(t)

	// Create a host.
	createBody := `{"nickname":"E2E-Host","token":"e2e-token"}`
	resp, err := http.Post(s.url("/api/hosts"), "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("POST /api/hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/hosts status = %d, body = %s", resp.StatusCode, body)
	}

	var host store.Host
	if err := json.NewDecoder(resp.Body).Decode(&host); err != nil {
		t.Fatalf("decode host: %v", err)
	}
	if host.Nickname != "E2E-Host" || host.Token != "e2e-token" {
		t.Fatalf("host = %#v", host)
	}

	// List hosts.
	resp, err = http.Get(s.url("/api/hosts"))
	if err != nil {
		t.Fatalf("GET /api/hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/hosts status = %d", resp.StatusCode)
	}
	var hosts []map[string]any
	json.NewDecoder(resp.Body).Decode(&hosts)
	if len(hosts) != 1 {
		t.Fatalf("hosts len = %d, want 1", len(hosts))
	}

	// Get single host.
	resp, err = http.Get(s.url("/api/hosts/" + host.PublicID))
	if err != nil {
		t.Fatalf("GET /api/hosts/%s: %v", host.PublicID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/hosts/%s status = %d", host.PublicID, resp.StatusCode)
	}

	// Update host.
	patchBody := `{"nickname":"E2E-Updated"}`
	req, _ := http.NewRequest(http.MethodPatch, s.url("/api/hosts/"+host.PublicID), strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /api/hosts status = %d", resp.StatusCode)
	}

	// Delete host.
	req, _ = http.NewRequest(http.MethodDelete, s.url("/api/hosts/"+host.PublicID), nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/hosts status = %d", resp.StatusCode)
	}
}

func TestE2EAgentWebSocket(t *testing.T) {
	s := startE2EServer(t)

	// Create a host first.
	createBody := `{"nickname":"WS-Host","token":"ws-token"}`
	resp, err := http.Post(s.url("/api/hosts"), "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("POST /api/hosts: %v", err)
	}
	var host store.Host
	json.NewDecoder(resp.Body).Decode(&host)
	resp.Body.Close()

	// Connect via WebSocket.
	wsURL := "ws" + strings.TrimPrefix(s.url(""), "http") + "/report"
	header := http.Header{"Token": []string{host.Token}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	// Send a system_info message.
	sysInfo := map[string]any{
		"type": "system_info",
		"data": map[string]any{
			"hostname":     "e2e-test",
			"os":           "linux",
			"platform":     "ubuntu",
			"cores":        4,
			"hostId":       "e2e-host-id",
			"bootTime":     1000,
			"modelName":    "Test Machine",
			"kernelArch":   "x86_64",
			"platformVersion": "22.04",
		},
	}
	if err := conn.WriteJSON(sysInfo); err != nil {
		t.Fatalf("write system_info: %v", err)
	}

	// Send a system_usage message.
	now := time.Now().Unix()
	sysUsage := map[string]any{
		"type": "system_usage",
		"data": map[string]any{
			"cpuUsage":     42.5,
			"memoryTotal":  8589934592,
			"memoryUsed":   4294967296,
			"timestamp":    now,
			"load1":        1.5,
			"load5":        1.2,
			"load15":       0.8,
		},
	}
	if err := conn.WriteJSON(sysUsage); err != nil {
		t.Fatalf("write system_usage: %v", err)
	}

	// Give the server a moment to process.
	time.Sleep(200 * time.Millisecond)

	// Verify the host was updated with system info.
	resp, err = http.Get(s.url("/api/hosts/" + host.PublicID))
	if err != nil {
		t.Fatalf("GET /api/hosts: %v", err)
	}
	defer resp.Body.Close()
	var got map[string]any
	json.NewDecoder(resp.Body).Decode(&got)
	if got["hostname"] != "e2e-test" {
		t.Fatalf("hostname = %v, want e2e-test", got["hostname"])
	}
	if got["online"] != true {
		t.Fatalf("online = %v, want true", got["online"])
	}

	// Verify usage data is queryable.
	from := now - 60
	to := now + 60
	resp, err = http.Get(fmt.Sprintf(s.url("/api/hosts/%s/usage?from=%d&to=%d"), host.PublicID, from, to))
	if err != nil {
		t.Fatalf("GET usage: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET usage status = %d", resp.StatusCode)
	}
	var usages []map[string]any
	json.NewDecoder(resp.Body).Decode(&usages)
	if len(usages) == 0 {
		t.Fatal("no usage data returned")
	}
}

func TestE2ESSEEvents(t *testing.T) {
	s := startE2EServer(t)

	resp, err := http.Get(s.url("/events"))
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /events status = %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read the retry hint within a timeout (ping comes every 25s, too slow).
	buf := make([]byte, 1024)
	deadline := time.Now().Add(5 * time.Second)
	var got strings.Builder
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				got.Write(buf[:n])
			}
			if err != nil {
				return
			}
			if strings.Contains(got.String(), "retry:") {
				return
			}
		}
	}()
	select {
	case <-readDone:
	case <-time.After(time.Until(deadline)):
		t.Fatalf("SSE stream timed out, got: %s", got.String())
	}
	if !strings.Contains(got.String(), "retry:") {
		t.Fatalf("SSE stream missing retry, got: %s", got.String())
	}
}

func TestE2EBodySizeLimit(t *testing.T) {
	s := startE2EServer(t)

	// Send a request with a body > 1 MiB.
	big := `{"nickname":"` + strings.Repeat("x", 2<<20) + `","token":"x"}`
	resp, err := http.Post(s.url("/api/hosts"), "application/json", strings.NewReader(big))
	if err != nil {
		t.Fatalf("POST oversized: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestE2ENotificationSSRF(t *testing.T) {
	s := startE2EServer(t)

	cases := []struct {
		name string
		url  string
		code int
	}{
		{"private IP", "http://10.0.0.1/send?text=%text%", http.StatusBadRequest},
		{"localhost", "http://127.0.0.1:9999/send?text=%text%", http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"tag":"e2e","url":"%s","notifyOnline":true}`, tc.url)
			resp, err := http.Post(s.url("/api/notifications"), "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatalf("POST /api/notifications: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.code {
				got, _ := io.ReadAll(resp.Body)
				t.Fatalf("%s status = %d, want %d, body = %s", tc.name, resp.StatusCode, tc.code, got)
			}
		})
	}
}

func TestE2EGracefulShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "swallow-monitor")

	build := exec.Command("go", "build", "-trimpath", "-o", binPath, ".")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	dbPath := filepath.Join(tmpDir, "swallow.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfg := fmt.Sprintf("listen: %q\ndbPath: %q\n", addr, dbPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(binPath, "-c", cfgPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for ready.
	baseURL := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/me")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Send SIGTERM.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server exited with error: %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-done
		t.Fatal("server did not shut down within 10s")
	}

	// Verify the server is no longer reachable.
	_, err := http.Get(baseURL + "/api/me")
	if err == nil {
		t.Fatal("server still reachable after shutdown")
	}
}

func TestE2ESettingsAndTags(t *testing.T) {
	s := startE2EServer(t)

	// Get default settings.
	resp, err := http.Get(s.url("/api/settings"))
	if err != nil {
		t.Fatalf("GET /api/settings: %v", err)
	}
	defer resp.Body.Close()
	var settings store.SiteSettings
	json.NewDecoder(resp.Body).Decode(&settings)
	if settings.SiteName != "SwallowMonitor" {
		t.Fatalf("default siteName = %q, want SwallowMonitor", settings.SiteName)
	}

	// Update settings.
	patchBody := `{"siteName":"E2E监控","siteDescription":"端到端测试"}`
	req, _ := http.NewRequest(http.MethodPatch, s.url("/api/settings"), strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /api/settings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PATCH /api/settings status = %d, body = %s", resp.StatusCode, body)
	}

	// Create a tag.
	resp, err = http.Post(s.url("/api/tags"), "application/json", strings.NewReader(`{"name":"e2e"}`))
	if err != nil {
		t.Fatalf("POST /api/tags: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/tags status = %d", resp.StatusCode)
	}

	// List tags.
	resp, err = http.Get(s.url("/api/tags"))
	if err != nil {
		t.Fatalf("GET /api/tags: %v", err)
	}
	defer resp.Body.Close()
	var tags []store.Tag
	json.NewDecoder(resp.Body).Decode(&tags)
	if len(tags) != 1 || tags[0].Name != "e2e" {
		t.Fatalf("tags = %#v", tags)
	}
}