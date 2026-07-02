package test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	_ "modernc.org/sqlite"
)

func TestTagStoreCRUDAndHostAssociation(t *testing.T) {
	st, _ := newTestApp(t, nil)

	prod, err := st.CreateTag("prod")
	if err != nil {
		t.Fatalf("CreateTag(prod) error = %v", err)
	}
	web, err := st.CreateTag("web")
	if err != nil {
		t.Fatalf("CreateTag(web) error = %v", err)
	}
	if prod.ID == 0 || web.ID == 0 {
		t.Fatalf("created tag ids = %d, %d", prod.ID, web.ID)
	}

	host, err := st.CreateHost("Web-01", "token-tags", []string{"prod", "web", "missing"})
	if err != nil {
		t.Fatalf("CreateHost() error = %v", err)
	}
	got, err := st.GetHost(host.PublicID)
	if err != nil {
		t.Fatalf("GetHost() error = %v", err)
	}
	if strings.Join(got.Tags, ",") != "prod,web" {
		t.Fatalf("host tags = %#v, want prod/web only", got.Tags)
	}

	if err := st.UpdateTag(prod.ID, "production"); err != nil {
		t.Fatalf("UpdateTag() error = %v", err)
	}
	got, err = st.GetHost(host.PublicID)
	if err != nil {
		t.Fatalf("GetHost() after tag rename error = %v", err)
	}
	if strings.Join(got.Tags, ",") != "production,web" {
		t.Fatalf("host tags after rename = %#v", got.Tags)
	}

	if err := st.DeleteTag(web.ID); err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}
	got, err = st.GetHost(host.PublicID)
	if err != nil {
		t.Fatalf("GetHost() after tag delete error = %v", err)
	}
	if strings.Join(got.Tags, ",") != "production" {
		t.Fatalf("host tags after delete = %#v", got.Tags)
	}
}

func TestTagAPIValidationAndCRUD(t *testing.T) {
	_, mux := newTestApp(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/tags", strings.NewReader(`{"name":"prod"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/tags status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created store.Tag
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created tag: %v", err)
	}
	if created.Name != "prod" || created.ID == 0 {
		t.Fatalf("created tag = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/tags status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var tags []store.Tag
	if err := json.NewDecoder(rec.Body).Decode(&tags); err != nil {
		t.Fatalf("decode tags: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "prod" {
		t.Fatalf("tags = %#v", tags)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/tags/"+strconvID(created.ID), strings.NewReader(`{"name":"production"}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/tags status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/tags", strings.NewReader(`{"name":"   "}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty tag status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/tags/"+strconvID(created.ID), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/tags status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestTagAPIRequiresAuth(t *testing.T) {
	_, mux := newTestApp(t, &model.Config{GitHub: model.GitHubConfig{ClientID: "client"}})

	req := httptest.NewRequest(http.MethodPost, "/api/tags", strings.NewReader(`{"name":"prod"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST without session status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHostAPIUsesExistingTagsOnly(t *testing.T) {
	st, mux := newTestApp(t, nil)
	if _, err := st.CreateTag("prod"); err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/hosts", strings.NewReader(`{"nickname":"Web-01","token":"token-api-tags","tags":["prod","missing"]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/hosts status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var host store.Host
	if err := json.NewDecoder(rec.Body).Decode(&host); err != nil {
		t.Fatalf("decode host: %v", err)
	}
	if strings.Join(host.Tags, ",") != "prod" {
		t.Fatalf("created host tags = %#v, want prod only", host.Tags)
	}
}

func TestOpenMigratesLegacyHostTagsWithoutDeadlock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	_, err = db.Exec(`CREATE TABLE hosts (
		public_id TEXT PRIMARY KEY,
		token TEXT UNIQUE NOT NULL,
		nickname TEXT NOT NULL,
		tags TEXT,
		host_id TEXT,
		hostname TEXT,
		os TEXT,
		platform TEXT,
		platform_version TEXT,
		kernel_arch TEXT,
		model_name TEXT,
		cores INTEGER,
		virtualization_role TEXT,
		boot_time INTEGER,
		last_info_json TEXT,
		last_seen INTEGER,
		created_at INTEGER
	)`)
	if err != nil {
		t.Fatalf("create legacy hosts table error = %v", err)
	}
	_, err = db.Exec(`INSERT INTO hosts(public_id, token, nickname, tags, created_at) VALUES('legacy-host', 'legacy-token', 'Legacy', '["prod","web"]', 1)`)
	if err != nil {
		t.Fatalf("insert legacy host error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		st, err := store.Open(dbPath)
		if err != nil {
			done <- err
			return
		}
		defer st.Close()
		host, err := st.GetHost("legacy-host")
		if err != nil {
			done <- err
			return
		}
		if strings.Join(host.Tags, ",") != "prod,web" {
			done <- errUnexpectedTags(host.Tags)
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Open legacy db error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Open legacy db timed out, possible migration deadlock")
	}
}

func errUnexpectedTags(tags []string) error {
	return fmt.Errorf("host tags = %#v, want prod/web", tags)
}
