package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func releaseTestClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	c := newAgentReleaseClient()
	c.Transport = releaseTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != agentReleasesURL || r.Method != http.MethodGet {
			t.Errorf("untrusted upstream request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials forwarded")
		}
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Result(), nil
	})
	return c
}

func TestAgentReleasesPermissionsAndMethod(t *testing.T) {
	s := securityAuthServer()
	var calls atomic.Int32
	s.agentReleases.client = releaseTestClient(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); io.WriteString(w, "[]") })
	for _, tc := range []struct {
		method string
		login  bool
		status int
	}{
		{"GET", false, 401}, {"POST", true, 405}, {"HEAD", true, 405}, {"PUT", false, 405}, {"DELETE", true, 405}, {"OPTIONS", true, 405}, {"GET", true, 200},
	} {
		r := httptest.NewRequest(tc.method, "/api/agent/releases?url=http://127.0.0.1/private&endpoint=https://example.invalid&per_page=999&page=9", nil)
		if tc.login {
			r.AddCookie(&http.Cookie{Name: s.sessionCookieName(), Value: s.sessions.create("admin")})
		}
		w := httptest.NewRecorder()
		s.handleAPI(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s login=%v: %d", tc.method, tc.login, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable response")
		}
		if tc.status == 200 {
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 4 || body["latestVersion"] != "" || body["stale"] != false || body["checkedAt"].(float64) <= 0 || len(body["releases"].([]any)) != 0 {
				t.Fatalf("empty contract: %s", w.Body)
			}
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls: %d", calls.Load())
	}
	s.oauth = nil
	w := httptest.NewRecorder()
	s.handleAPI(w, httptest.NewRequest("GET", "/api/agent/releases", nil))
	if w.Code != 401 {
		t.Fatal("unconfigured OAuth failed open")
	}
	s.cfg.InsecureDevelopment = true
	w = httptest.NewRecorder()
	s.handleAPI(w, httptest.NewRequest("GET", "/api/agent/releases", nil))
	if w.Code != 200 {
		t.Fatal("development management denied")
	}
}

func TestAgentReleasesFilterSortAssets(t *testing.T) {
	tags := []string{"v1.9.0", "v1.10.0", "v2.0.0", "v99999999999999999999999999999.0.0", "v99999999999999999999999999998.0.0", "v1.10.1+build.01", "v1.10.1+other", "v01.2.3", "v1.2", "v1x2x3", "v1.2.3-rc.1", "../v1.2.3", "v1.2.3+", "v1.2.3+build..x", "v1.2.3;id", "1.2.3"}
	var input []map[string]any
	for _, tag := range tags {
		input = append(input, map[string]any{"tag_name": tag})
	}
	input = append(input, map[string]any{"tag_name": "v500.0.0", "draft": true}, map[string]any{"tag_name": "v600.0.0", "prerelease": true})
	assets := []map[string]string{}
	add := func(name, state, path string) {
		assets = append(assets, map[string]string{"name": name, "state": state, "browser_download_url": path})
	}
	for _, name := range []string{"bootstrap.sh", "install.sh", "migrate-token-to-key.sh", "upgrade.sh", "checksums.txt", "swallow-agent-linux-amd64"} {
		add(name, "uploaded", agentDownloadBase+"v1.9.0/"+name)
	}
	add("bootstrap.sh", "uploaded", agentDownloadBase+"v1.9.0/bootstrap.sh")
	add("pending", "new", agentDownloadBase+"v1.9.0/pending")
	add("evil", "uploaded", "https://example.invalid/evil")
	add("other-tag", "uploaded", agentDownloadBase+"v2.0.0/other-tag")
	add("query", "uploaded", agentDownloadBase+"v1.9.0/query?x=1")
	add("../bad", "uploaded", agentDownloadBase+"v1.9.0/../bad")
	add("http", "uploaded", strings.Replace(agentDownloadBase, "https:", "http:", 1)+"v1.9.0/http")
	input[0]["assets"] = assets
	c := releaseTestClient(t, func(w http.ResponseWriter, _ *http.Request) { json.NewEncoder(w).Encode(input) })
	got, err := fetchAgentReleases(c)
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, r := range got {
		versions = append(versions, r.Version)
		if r.Assets == nil {
			t.Fatal("null assets")
		}
	}
	want := []string{tags[3], tags[4], tags[2], tags[5], tags[6], tags[1], tags[0]}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("versions: %v", versions)
	}
	if len(got[0].Assets) != 0 || len(got[len(got)-1].Assets) != 6 {
		t.Fatalf("assets: %+v", got)
	}
	if agentVersionGreater("v1.2.3+abc", "v1.2.3+def") || agentVersionGreater("v1.2.3+def", "v1.2.3+abc") {
		t.Fatal("build metadata affected precedence")
	}
	for _, pair := range [][2]string{{"v1.999999999999999999999.0", "v1.999999999999999999998.0"}, {"v1.2.100000000000000000000", "v1.2.99999999999999999999"}} {
		if !agentVersionGreater(pair[0], pair[1]) {
			t.Fatal("large number ordering")
		}
	}
}

func TestAgentReleasesCacheConcurrentFailureRecovery(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	c := agentReleaseCache{client: releaseTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(15 * time.Millisecond)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		io.WriteString(w, `[{"tag_name":"v2.0.0"}]`)
	})}
	batch := func(stale bool) {
		t.Helper()
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got := c.get()
				if got.Stale != stale || got.LatestVersion != "v2.0.0" || (got.Error != "") != stale {
					t.Errorf("response: %+v", got)
				}
			}()
		}
		wg.Wait()
	}
	batch(false)
	checked := c.response.CheckedAt
	if calls.Load() != 1 || time.Until(c.nextCheck) < 9*time.Minute {
		t.Fatal("miss not merged/cached")
	}
	batch(false)
	if calls.Load() != 1 {
		t.Fatal("cache miss")
	}
	c.nextCheck = time.Time{}
	fail.Store(true)
	batch(true)
	batch(true)
	if calls.Load() != 2 || c.response.CheckedAt != checked || time.Until(c.nextCheck) > time.Minute || time.Until(c.nextCheck) < 50*time.Second {
		t.Fatal("failure cooldown or stale timestamp")
	}
	c.nextCheck = time.Time{}
	fail.Store(false)
	batch(false)
	if calls.Load() != 3 {
		t.Fatal("recovery missing")
	}
}

func TestAgentReleasesEncodedAssetPaths(t *testing.T) {
	for _, tc := range []struct {
		suffix string
		valid  bool
	}{
		{"v1.2.3+build/upgrade.sh", true},
		{"v1.2.3%2Bbuild/upgrade.sh", true},
		{"v1.2.3%252Bbuild/upgrade.sh", false},
		{"v1.2.3+build/upgrade.sh?", false},
		{"v1.2.3+build/upgrade.sh#", false},
		{"v1.2.3+build/upgrade.sh#fragment", false},
		{"v1.2.3+build/../upgrade.sh", false},
	} {
		c := releaseTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]any{map[string]any{"tag_name": "v1.2.3+build", "assets": []any{map[string]string{"name": "upgrade.sh", "state": "uploaded", "browser_download_url": agentDownloadBase + tc.suffix}}}})
		})
		got, err := fetchAgentReleases(c)
		if err != nil || len(got) != 1 || (len(got[0].Assets) == 1) != tc.valid {
			t.Fatalf("%s: %+v %v", tc.suffix, got, err)
		}
	}
	for _, host := range []string{"user@github.com", "github.com:443", "github.com.evil.invalid"} {
		c := releaseTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]any{map[string]any{"tag_name": "v1.2.3", "assets": []any{map[string]string{"name": "upgrade.sh", "state": "uploaded", "browser_download_url": strings.Replace(agentDownloadBase, "github.com", host, 1) + "v1.2.3/upgrade.sh"}}}})
		})
		got, err := fetchAgentReleases(c)
		if err != nil || len(got[0].Assets) != 0 {
			t.Fatalf("host %s accepted", host)
		}
	}
}

func TestAgentReleasesFailuresAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"rate-limit", "secret", 429}, {"server", "secret", 500}, {"redirect", "", 302}, {"malformed", "[", 200}, {"object", "{}", 200}, {"null", "null", 200}, {"trailing", "[] {}", 200}, {"oversized", strings.Repeat(" ", agentReleaseBodyLimit+1), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := agentReleaseCache{client: releaseTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Location", "http://127.0.0.1/private")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})}
			for i := 0; i < 2; i++ {
				got := c.get()
				if !got.Stale || got.Error == "" || strings.Contains(got.Error, "secret") || got.CheckedAt != 0 || got.LatestVersion != "" || got.Releases == nil || len(got.Releases) != 0 {
					t.Fatalf("failure contract: %+v", got)
				}
			}
			if calls != 1 {
				t.Fatal("failure not cached or redirect followed")
			}
		})
	}
	c := newAgentReleaseClient()
	if c.Timeout != 10*time.Second {
		t.Fatal("missing timeout")
	}
	c.Timeout = 20 * time.Millisecond
	c.Transport = releaseTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	start := time.Now()
	if _, err := fetchAgentReleases(c); err == nil || time.Since(start) > time.Second {
		t.Fatal("timeout not enforced")
	}
}
