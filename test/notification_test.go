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
	waitForNotification(t, &mu, &queries, 2)

	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()
	if !strings.Contains(got[0], "Web-01") || !strings.Contains(got[0], "上线") {
		t.Fatalf("online text = %q", got[0])
	}
	if !strings.Contains(got[1], "Web-01") || !strings.Contains(got[1], "离线") {
		t.Fatalf("offline text = %q", got[1])
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
