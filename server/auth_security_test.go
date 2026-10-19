package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
	"golang.org/x/oauth2"
)

func securityAuthServer() *Server {
	return &Server{cfg: &model.Config{PublicURL: "https://monitor.example"}, sessions: newSessionStore(), oauth: &oauth2.Config{Endpoint: oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize"}}}
}

func TestSecurityStateBrowserBinding(t *testing.T) {
	for _, secure := range []bool{true, false} {
		s := securityAuthServer()
		if !secure {
			s.cfg.PublicURL = "http://monitor.example"
		}
		w := httptest.NewRecorder()
		s.handleLogin(w, httptest.NewRequest(http.MethodGet, "/login", nil))
		if w.Code != http.StatusFound {
			t.Fatalf("login: %d", w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("login response is cacheable")
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("cookies: %v", cookies)
		}
		c := cookies[0]
		location, _ := url.Parse(w.Header().Get("Location"))
		state := location.Query().Get("state")
		wantName := secureStateCookie
		if !secure {
			wantName = stateCookie
		}
		if c.Name != wantName || c.Value != state || state == "" || !c.HttpOnly || c.Secure != secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 600 || c.Path != "/" || c.Domain != "" {
			t.Fatalf("unsafe cookie: %+v", c)
		}
		for _, value := range []string{"", "wrong-browser", state, state} {
			r := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+state, nil)
			if value != "" {
				r.AddCookie(&http.Cookie{Name: wantName, Value: value})
			}
			w = httptest.NewRecorder()
			s.handleCallback(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("callback: %d", w.Code)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("callback response is cacheable")
			}
			if value == "" || value == "wrong-browser" {
				if _, ok := s.sessions.states[state]; !ok {
					t.Fatal("another browser consumed state")
				}
			}
		}
		if !strings.Contains(w.Body.String(), "invalid state") {
			t.Fatal("state replay accepted")
		}
		expired := s.sessions.newState()
		s.sessions.states[expired] = time.Now().Add(-time.Second)
		if s.sessions.consumeState(expired) {
			t.Fatal("expired state accepted")
		}
	}
}

func TestSecurityRejectsDuplicateAuthCookies(t *testing.T) {
	s := securityAuthServer()
	id := s.sessions.create("alice")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Cookie", secureSessionCookie+"="+id+"; "+secureSessionCookie+"=attacker")
	if _, ok := s.currentUser(r); ok {
		t.Fatal("duplicate session cookies accepted")
	}

	state := s.sessions.newState()
	r = httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+state, nil)
	r.Header.Set("Cookie", secureStateCookie+"="+state+"; "+secureStateCookie+"=attacker")
	w := httptest.NewRecorder()
	s.handleCallback(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("duplicate state cookies returned %d", w.Code)
	}
	if _, ok := s.sessions.states[state]; !ok {
		t.Fatal("duplicate state cookies consumed OAuth state")
	}
}

func TestSecurityAuthStoreBoundsAndCleanup(t *testing.T) {
	ss := newSessionStore()
	future := time.Now().Add(time.Hour)
	for i := 0; i < maxAuthEntries; i++ {
		key := fmt.Sprint(i)
		ss.states[key] = future
		ss.sessions[key] = session{user: key, expires: future}
		ss.loginWindows[key] = loginWindow{count: 1, expires: future}
	}
	if ss.newState() != "" || ss.create("user") != "" || ss.allowLogin("new-ip") {
		t.Fatal("capacity exceeded")
	}
	past := time.Now().Add(-time.Second)
	for key := range ss.states {
		ss.states[key] = past
		ss.sessions[key] = session{expires: past}
		ss.loginWindows[key] = loginWindow{expires: past}
	}
	ss.nextCleanup = time.Time{}
	if !ss.allowLogin("new-ip") || len(ss.states) != 0 || len(ss.sessions) != 0 || len(ss.loginWindows) != 1 {
		t.Fatal("expired entries not reclaimed")
	}
	if ss.newState() == "" || ss.create("user") == "" {
		t.Fatal("capacity not recovered")
	}
}

func TestSecurityLoginRateLimit(t *testing.T) {
	s := securityAuthServer()
	for i := 0; i <= loginAttemptsPerMinute; i++ {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/login", nil)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i))
		s.handleLogin(w, r)
		want := http.StatusFound
		if i == loginAttemptsPerMinute {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("attempt %d: got %d want %d", i, w.Code, want)
		}
	}
	if len(s.sessions.states) != loginAttemptsPerMinute {
		t.Fatal("rate-limited request allocated state")
	}
	for ip := range s.sessions.loginWindows {
		s.sessions.loginWindows[ip] = loginWindow{expires: time.Now().Add(-time.Second)}
	}
	w := httptest.NewRecorder()
	s.handleLogin(w, httptest.NewRequest(http.MethodGet, "/login", nil))
	if w.Code != http.StatusFound {
		t.Fatal("limit did not reset")
	}
}

func TestSecurityLogout(t *testing.T) {
	for _, tc := range []struct {
		method, origin, referer string
		want                    int
	}{
		{"GET", "https://monitor.example", "", 405},
		{"POST", "", "", 403},
		{"POST", "null", "https://monitor.example/", 403},
		{"POST", "https://evil.example", "", 403},
		{"POST", "http://monitor.example", "", 403},
		{"POST", "https://monitor.example:444", "", 403},
		{"POST", "https://monitor.example", "", 303},
		{"POST", "", "https://monitor.example/dashboard", 303},
	} {
		s := securityAuthServer()
		id := s.sessions.create("alice")
		r := httptest.NewRequest(tc.method, "/logout", nil)
		r.AddCookie(&http.Cookie{Name: secureSessionCookie, Value: id})
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Referer", tc.referer)
		w := httptest.NewRecorder()
		s.handleLogout(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: %d", tc, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("auth response is cacheable: %+v", tc)
		}
		if tc.want == http.StatusSeeOther {
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != secureSessionCookie || cookies[0].MaxAge != -1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" {
				t.Fatalf("session cookie not deleted safely: %+v", cookies)
			}
		}
		_, exists := s.sessions.get(id)
		if exists != (tc.want != 303) {
			t.Fatalf("unexpected session state: %+v", tc)
		}
	}
}

type securityRoundTripper func(*http.Request) (*http.Response, error)

func (f securityRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSecurityGitHubIdentity(t *testing.T) {
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	for _, tc := range []struct {
		status int
		body   string
		valid  bool
	}{
		{200, `{"login":"alice"}`, true},
		{401, `{"login":"alice"}`, false},
		{500, `{"login":"alice"}`, false},
		{200, `{}`, false},
		{200, `{"login":" "}`, false},
		{200, `invalid`, false},
	} {
		http.DefaultClient = &http.Client{Transport: securityRoundTripper(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
		})}
		login, err := securityAuthServer().fetchGitHubLogin(context.Background(), "token")
		if (err == nil) != tc.valid || (tc.valid && login != "alice") {
			t.Fatalf("%+v: %q %v", tc, login, err)
		}
	}
}

func TestSecurityAllowlistAndNoOAuth(t *testing.T) {
	s := securityAuthServer()
	if s.userAllowed("alice") {
		t.Fatal("empty allowlist grants access")
	}
	s.cfg.GitHub.AllowedUsers = []string{"alice", ""}
	if !s.userAllowed("alice") || s.userAllowed("bob") || s.userAllowed("") {
		t.Fatal("allowlist not enforced")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if s.canManage(r) {
		t.Fatal("anonymous OAuth management allowed")
	}
	s.oauth = nil
	if s.canManage(r) {
		t.Fatal("no-OAuth production management allowed")
	}
	w := httptest.NewRecorder()
	if s.requireUser(w, httptest.NewRequest(http.MethodPost, "/api/hosts", nil)) || w.Code != http.StatusUnauthorized {
		t.Fatal("no-OAuth production write allowed")
	}
	s.cfg.InsecureDevelopment = true
	if !s.canManage(r) {
		t.Fatal("explicit development mode rejected")
	}
}

func TestSecurityManagementOrigin(t *testing.T) {
	s := securityAuthServer()
	id := s.sessions.create("alice")
	for _, tc := range []struct {
		origin, site string
		allowed      bool
	}{
		{"", "", true},
		{"https://monitor.example", "same-origin", true},
		{"https://evil.example", "", false},
		{"", "cross-site", false},
		{"", "same-site", false},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/hosts", nil)
		r.AddCookie(&http.Cookie{Name: secureSessionCookie, Value: id})
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		if s.requireUser(w, r) != tc.allowed {
			t.Fatalf("origin check: %+v", tc)
		}
	}
}
