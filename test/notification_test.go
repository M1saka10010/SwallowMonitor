package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
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

func TestNotificationLocalhostRejected(t *testing.T) {
	_, mux := newTestApp(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"test","url":"http://127.0.0.1:8080/send?text=%text%","notifyOnline":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/notifications with localhost status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
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
	payload := `{"tag":"` + strings.Repeat("x", 512<<10) + `","url":"http://8.8.8.8/send?text=%text%","notifyOnline":true}`
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

	// First create a valid rule on a public IP.
	req := httptest.NewRequest(http.MethodPost, "/api/notifications", strings.NewReader(`{"tag":"test","url":"http://8.8.8.8/send?text=%text%","notifyOnline":true}`))
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
