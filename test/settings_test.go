package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/server"
	"github.com/M1saka10010/SwallowMonitor/store"
)

func newTestApp(t *testing.T, cfg *model.Config) (*store.Store, *http.ServeMux) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if cfg == nil {
		cfg = &model.Config{}
	}
	mux := http.NewServeMux()
	server.New(cfg, st, mux, http.NotFoundHandler())
	return st, mux
}

func TestSiteSettingsDefaultsAndUpdate(t *testing.T) {
	st, _ := newTestApp(t, nil)

	settings, err := st.GetSiteSettings()
	if err != nil {
		t.Fatalf("GetSiteSettings() error = %v", err)
	}
	if settings.SiteName != "SwallowMonitor" {
		t.Fatalf("default SiteName = %q, want %q", settings.SiteName, "SwallowMonitor")
	}
	if settings.SiteDescription != "" {
		t.Fatalf("default SiteDescription = %q, want empty", settings.SiteDescription)
	}

	want := store.SiteSettings{SiteName: "我的监控", SiteDescription: "服务器状态监控"}
	if err := st.UpdateSiteSettings(want); err != nil {
		t.Fatalf("UpdateSiteSettings() error = %v", err)
	}

	got, err := st.GetSiteSettings()
	if err != nil {
		t.Fatalf("GetSiteSettings() after update error = %v", err)
	}
	if got != want {
		t.Fatalf("settings = %#v, want %#v", got, want)
	}
}

func TestSettingsAPIGetAndPatch(t *testing.T) {
	_, mux := newTestApp(t, nil)

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(`{"siteName":"我的监控","siteDescription":"服务器状态监控"}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	mux.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/settings status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings status = %d, body = %s", getRec.Code, getRec.Body.String())
	}

	var got store.SiteSettings
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("decode settings response: %v", err)
	}
	if got.SiteName != "我的监控" || got.SiteDescription != "服务器状态监控" {
		t.Fatalf("settings = %#v", got)
	}
}

func TestSettingsAPIValidation(t *testing.T) {
	_, mux := newTestApp(t, nil)

	longName := strings.Repeat("a", 61)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(`{"siteName":"`+longName+`"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long name status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(`{"siteName":"   "}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSettingsAPIPatchRequiresAuth(t *testing.T) {
	_, mux := newTestApp(t, &model.Config{GitHub: model.GitHubConfig{ClientID: "client"}})

	req := httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(`{"siteName":"我的监控"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PATCH without session status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
