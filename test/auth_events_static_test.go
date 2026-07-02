package test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/server"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/M1saka10010/SwallowMonitor/web"
	"github.com/gorilla/websocket"
)

func TestMeLoginLogoutAuthStates(t *testing.T) {
	_, openMux := newTestApp(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	openMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"loggedIn":true`) || !strings.Contains(rec.Body.String(), `"authEnabled":false`) {
		t.Fatalf("open /api/me status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	rec = httptest.NewRecorder()
	openMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("/login without oauth status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}

	_, authMux := newTestApp(t, &model.Config{PublicURL: "http://example.test", GitHub: model.GitHubConfig{ClientID: "client", AllowedUsers: []string{"octocat"}}})
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec = httptest.NewRecorder()
	authMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"loggedIn":false`) || !strings.Contains(rec.Body.String(), `"authEnabled":true`) {
		t.Fatalf("auth /api/me anonymous status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/logout", nil)
	rec = httptest.NewRecorder()
	authMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("/logout status = %d, want %d", rec.Code, http.StatusFound)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "swallow_session" || cookies[0].MaxAge != -1 {
		t.Fatalf("logout cookies = %#v", cookies)
	}
}

func TestEventsStreamsRetryStatusAndUsageData(t *testing.T) {
	st, mux := newTestApp(t, nil)
	host, err := st.CreateHost("Web-01", "token-events", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()

	resp, err := appSrv.Client().Get(appSrv.URL + "/events")
	if err != nil {
		t.Fatalf("GET /events error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /events status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	readDone := make(chan string, 1)
	go readUntilContains(resp.Body, readDone, `"type":"usage"`)

	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Token": []string{host.Token}})
	if err != nil {
		t.Fatalf("dial report websocket: %v", err)
	}
	defer conn.Close()

	usageData := []byte(`{"cpuUsage":42,"timestamp":1234}`)
	if err := conn.WriteJSON(model.Envelope{Type: model.TypeSystemUsage, Data: usageData}); err != nil {
		t.Fatalf("write usage envelope: %v", err)
	}

	select {
	case got := <-readDone:
		if !strings.Contains(got, "retry: 3000") || !strings.Contains(got, `"type":"status"`) || !strings.Contains(got, `"type":"usage"`) {
			t.Fatalf("/events stream = %q, want retry, status and usage events", got)
		}
	case <-time.After(2 * time.Second):
		resp.Body.Close()
		got := <-readDone
		t.Fatalf("/events stream = %q, want usage event", got)
	}
}

func TestStaticHandlerServesEmbeddedFiles(t *testing.T) {
	mux := http.NewServeMux()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	server.New(&model.Config{}, st, mux, web.Handler())

	for _, tc := range []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/", contentType: "text/html", contains: "app.js"},
		{path: "/app.js", contentType: "text/javascript", contains: "fetch"},
		{path: "/style.css", contentType: "text/css", contains: "body"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, tc.contentType) {
				t.Fatalf("Content-Type = %q, want containing %q", contentType, tc.contentType)
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("response for %s does not contain %q", tc.path, tc.contains)
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/missing.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing static status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func readUntilContains(r io.Reader, done chan<- string, marker string) {
	buf := make([]byte, 256)
	var got string
	for {
		n, err := r.Read(buf)
		if n > 0 {
			got += string(buf[:n])
		}
		if strings.Contains(got, marker) {
			done <- got
			return
		}
		if err != nil {
			done <- got
			return
		}
	}
}
