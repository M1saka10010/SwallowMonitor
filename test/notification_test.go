package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
)

func TestNotificationRuleStoreCRUDAndMatching(t *testing.T) {
	st, _ := newTestApp(t, nil)

	rules, err := st.ListNotificationRules()
	if err != nil {
		t.Fatalf("ListNotificationRules() error = %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("initial rules len = %d, want 0", len(rules))
	}

	created, err := st.CreateNotificationRule(store.NotificationRule{
		Tag:           "prod",
		URL:           "https://example.com/send?text=%text%",
		NotifyOnline:  true,
		NotifyOffline: false,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatal("created rule ID = 0")
	}

	matched, err := st.MatchingNotificationRules([]string{"prod", "web"}, "online")
	if err != nil {
		t.Fatalf("MatchingNotificationRules() error = %v", err)
	}
	if len(matched) != 1 || matched[0].ID != created.ID {
		t.Fatalf("online matched = %#v, want created rule", matched)
	}

	matched, err = st.MatchingNotificationRules([]string{"prod"}, "offline")
	if err != nil {
		t.Fatalf("MatchingNotificationRules(offline) error = %v", err)
	}
	if len(matched) != 0 {
		t.Fatalf("offline matched len = %d, want 0", len(matched))
	}

	created.Tag = ""
	created.NotifyOffline = true
	created.Enabled = true
	if err := st.UpdateNotificationRule(created); err != nil {
		t.Fatalf("UpdateNotificationRule() error = %v", err)
	}
	matched, err = st.MatchingNotificationRules([]string{"staging"}, "offline")
	if err != nil {
		t.Fatalf("MatchingNotificationRules(global) error = %v", err)
	}
	if len(matched) != 1 || matched[0].Tag != "" {
		t.Fatalf("global matched = %#v", matched)
	}

	if err := st.DeleteNotificationRule(created.ID); err != nil {
		t.Fatalf("DeleteNotificationRule() error = %v", err)
	}
	rules, err = st.ListNotificationRules()
	if err != nil {
		t.Fatalf("ListNotificationRules() after delete error = %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("rules after delete len = %d, want 0", len(rules))
	}
}

func TestNotificationAPIValidationAndCRUD(t *testing.T) {
	_, mux := newTestApp(t, nil)

	badReq := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"prod","url":"https://example.com/send"}`))
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("url without placeholder status = %d, want %d", badRec.Code, http.StatusBadRequest)
	}

	badReq = httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"prod","url":"ftp://example.com/send?text=%text%"}`))
	badRec = httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("non-http url status = %d, want %d", badRec.Code, http.StatusBadRequest)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"prod","url":"https://example.com/send?text=%text%","notifyOnline":true,"notifyOffline":false,"enabled":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/notifications status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created store.NotificationRule
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/notifications/"+strconvID(created.ID), strings.NewReader(`{"tag":"","url":"https://example.com/all?text=%text%","notifyOnline":false,"notifyOffline":true,"enabled":true}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/notifications status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/notifications status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rules []store.NotificationRule
	if err := json.NewDecoder(rec.Body).Decode(&rules); err != nil {
		t.Fatalf("decode rules: %v", err)
	}
	if len(rules) != 1 || rules[0].Tag != "" || rules[0].NotifyOnline || !rules[0].NotifyOffline {
		t.Fatalf("rules = %#v", rules)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/notifications/"+strconvID(created.ID), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/notifications status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestNotificationAPIRequiresAuth(t *testing.T) {
	_, mux := newTestApp(t, &model.Config{GitHub: model.GitHubConfig{ClientID: "client"}})

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"url":"https://example.com/send?text=%text%"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST without session status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestNotificationDispatchOnStatusChange(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	notifySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query().Get("text"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer notifySrv.Close()

	st, mux := newTestApp(t, nil)
	if _, err := st.CreateTag("prod"); err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}
	host, err := st.CreateHost("Web-01", "token-1", []string{"prod"})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	_, err = st.CreateNotificationRule(store.NotificationRule{
		Tag:           "prod",
		URL:           notifySrv.URL + "/send?text=%text%",
		NotifyOnline:  true,
		NotifyOffline: true,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"
	header := http.Header{"Token": []string{host.Token}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial report websocket: %v", err)
	}

	waitForNotification(t, &mu, &queries, 1)
	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	if !strings.Contains(got[0], "Web-01") || !strings.Contains(got[0], "上线") {
		t.Fatalf("online text = %q", got[0])
	}
	if len(got) != 1 {
		t.Fatalf("notification count shortly after disconnect = %d, want 1", len(got))
	}
}

func TestNoOnlineNotificationAfterReconnectWithinDelay(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	notifySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query().Get("text"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer notifySrv.Close()

	st, mux := newTestApp(t, nil)
	host, err := st.CreateHost("Web-02", "token-2", nil)
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	_, err = st.CreateNotificationRule(store.NotificationRule{
		Tag:           "",
		URL:           notifySrv.URL + "/send?text=%text%",
		NotifyOnline:  true,
		NotifyOffline: true,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule() error = %v", err)
	}

	appSrv := httptest.NewServer(mux)
	defer appSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(appSrv.URL, "http") + "/report"
	header := http.Header{"Token": []string{host.Token}}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("first dial report websocket: %v", err)
	}
	waitForNotification(t, &mu, &queries, 1)
	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)

	conn, _, err = websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("reconnect dial report websocket: %v", err)
	}
	defer conn.Close()
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("notification count after reconnect = %d, want 1 (reconnect within delay must be silent)", len(got))
	}
	if !strings.Contains(got[0], "上线") {
		t.Fatalf("notification text = %q, want 上线", got[0])
	}
}

func TestNotificationSSRFPrivateIPsRejected(t *testing.T) {
	_, mux := newTestApp(t, nil)

	cases := []struct {
		name string
		url  string
	}{
		{"10.x private", "http://10.0.0.1/send?text=%text%"},
		{"192.168.x private", "http://192.168.1.1/send?text=%text%"},
		{"172.16.x private", "http://172.16.0.1/send?text=%text%"},
		{"link-local", "http://169.254.169.254/send?text=%text%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"tag":"test","url":"` + tc.url + `","notifyOnline":true}`
			req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /api/notifications with %s status = %d, want %d, body = %s", tc.name, rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestNotificationLocalhostAllowed(t *testing.T) {
	_, mux := newTestApp(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"test","url":"http://127.0.0.1:8080/send?text=%text%","notifyOnline":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/notifications with localhost status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestRequestBodySizeLimit(t *testing.T) {
	_, mux := newTestApp(t, nil)

	big := `{"siteName":"` + strings.Repeat("a", 2<<20) + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(big))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH /api/settings with oversized body status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRequestBodySizeUnderLimitAccepted(t *testing.T) {
	_, mux := newTestApp(t, nil)

	// ~512 KiB body, well under the 1 MiB limit. Use notification endpoint
	// (settings endpoint rejects long siteName for its own validation).
	payload := `{"tag":"` + strings.Repeat("x", 512<<10) + `","url":"http://127.0.0.1:8080/send?text=%text%","notifyOnline":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/notifications with body under limit status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestNotificationSSRFDirectIPRejected(t *testing.T) {
	_, mux := newTestApp(t, nil)

	// Direct IPv4 private address, no DNS needed.
	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"test","url":"http://10.10.10.10/send?text=%text%","notifyOnline":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/notifications with direct private IP status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNotificationSSRFUnresolvableHostRejected(t *testing.T) {
	_, mux := newTestApp(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"test","url":"http://this-host-definitely-does-not-exist.invalid/send?text=%text%","notifyOnline":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/notifications with unresolvable host status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNotificationSSRFUpdatePathAlsoRejects(t *testing.T) {
	_, mux := newTestApp(t, nil)

	// First create a valid rule on localhost.
	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"test","url":"http://127.0.0.1:8080/send?text=%text%","notifyOnline":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create for update test: %d", rec.Code)
	}
	var created store.NotificationRule
	json.NewDecoder(rec.Body).Decode(&created)

	// Try to update the URL to a private IP — should be rejected.
	req = httptest.NewRequest(http.MethodPatch, "/api/notifications/"+strconvID(created.ID), strings.NewReader(`{"tag":"test","url":"http://192.168.1.1/send?text=%text%","notifyOnline":true}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH /api/notifications to private IP status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func waitForNotification(t *testing.T, mu *sync.Mutex, queries *[]string, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(*queries)
		mu.Unlock()
		if got >= count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("notification count = %d, want at least %d", len(*queries), count)
}
