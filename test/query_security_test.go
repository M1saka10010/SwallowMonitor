package test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
)

func TestUsageRejectsOverflowAndCancels(t *testing.T) {
	st, mux := newTestApp(t, nil)
	for _, query := range []string{"from=-9223372036854775808&to=9223372036854775807", "from=0&to=9223372036854775807", "from=-1&to=10"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/hosts/example/usage?"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d", query, w.Code)
		}
	}
	if _, err := st.QueryUsage("example", math.MinInt64, math.MaxInt64); err == nil {
		t.Fatal("store accepted overflowing range")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.QueryUsageContext(ctx, "example", 0, 10); err == nil {
		t.Fatal("query ignored cancellation")
	}
}

func TestDeletedHostCannotAcceptSamples(t *testing.T) {
	st, _ := newTestApp(t, nil)
	h, err := st.CreateHost("test", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteHost(h.PublicID); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertUsage(h.PublicID, &model.SystemUsage{Timestamp: 1}); err != store.ErrNotFound {
		t.Fatalf("insert after delete=%v", err)
	}
}

func TestJSONResponsesCannotBeCached(t *testing.T) {
	_, mux := newTestApp(t, nil)
	for _, path := range []string{"/api/me", "/api/hosts", "/api/notifications"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s not protected", path)
		}
	}
}

func TestPublicHostListIsBoundedAndIncludesLatestUsage(t *testing.T) {
	st, mux := newTestApp(t, &model.Config{GitHub: model.GitHubConfig{ClientID: "client"}})
	for i := 0; i < 501; i++ {
		h, err := st.CreateHost("host", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.InsertUsage(h.PublicID, &model.SystemUsage{Timestamp: 1, CPUUsage: 10}); err != nil {
			t.Fatal(err)
		}
		if err := st.InsertUsage(h.PublicID, &model.SystemUsage{Timestamp: 2, CPUUsage: 20}); err != nil {
			t.Fatal(err)
		}
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/hosts", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var hosts []struct {
		PublicID string             `json:"publicId"`
		Token    string             `json:"token"`
		Latest   *model.SystemUsage `json:"latest"`
	}
	if err := json.NewDecoder(w.Body).Decode(&hosts); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 500 {
		t.Fatalf("hosts len=%d, want 500", len(hosts))
	}
	for _, h := range hosts {
		if h.Token != "" {
			t.Fatalf("public response exposed token for %s", h.PublicID)
		}
		if h.Latest == nil || h.Latest.Timestamp != 2 || h.Latest.CPUUsage != 20 {
			t.Fatalf("latest=%#v, want timestamp 2 CPU 20", h.Latest)
		}
	}
}
