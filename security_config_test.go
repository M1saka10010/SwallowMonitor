package main

import (
	"github.com/M1saka10010/SwallowMonitor/model"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostGuardAllowsOnlyConfiguredAuthorityAndHealth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := publicURLHostHandler("https://monitor.example:8443", next)
	for _, tc := range []struct {
		host, path string
		want       int
	}{
		{"monitor.example:8443", "/api/me", http.StatusNoContent},
		{"MONITOR.EXAMPLE:8443", "/api/me", http.StatusNoContent},
		{"evil.example:8443", "/api/me", http.StatusMisdirectedRequest},
		{"127.0.0.1:8080", "/healthz", http.StatusOK},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("host=%q path=%q status=%d want=%d", tc.host, tc.path, w.Code, tc.want)
		}
	}
}

func TestSecurityConfiguration(t *testing.T) {
	badDev := model.Config{Listen: "127.0.0.1:8080", InsecureDevelopment: true, PublicURL: "https://monitor.example.com"}
	if validateSecurityConfig(&badDev) == nil {
		t.Fatal("mismatched development origin accepted")
	}
	for _, tc := range []struct {
		name  string
		cfg   model.Config
		valid bool
	}{
		{"defaults denied", model.Config{}, false},
		{"public development denied", model.Config{Listen: ":8080", InsecureDevelopment: true}, false},
		{"local development explicit", model.Config{Listen: "127.0.0.1:8080", InsecureDevelopment: true}, true},
		{"IPv6 development", model.Config{Listen: "[::1]:8080", InsecureDevelopment: true}, true},
		{"http production denied", model.Config{PublicURL: "http://monitor.example", GitHub: model.GitHubConfig{ClientID: "id", ClientSecret: "secret", AllowedUsers: []string{"admin"}}}, false},
		{"empty whitelist denied", model.Config{PublicURL: "https://monitor.example", GitHub: model.GitHubConfig{ClientID: "id", ClientSecret: "secret"}}, false},
		{"production", model.Config{PublicURL: "https://monitor.example", GitHub: model.GitHubConfig{ClientID: "id", ClientSecret: "secret", AllowedUsers: []string{"admin"}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSecurityConfig(&tc.cfg); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestPublicURLHostHandler(t *testing.T) {
	called := false
	h := publicURLHostHandler("http://127.0.0.1:8080", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	for _, tc := range []struct {
		host string
		want int
	}{
		{"127.0.0.1:8080", http.StatusOK},
		{"127.0.0.1", http.StatusMisdirectedRequest},
		{"localhost:8080", http.StatusMisdirectedRequest},
		{"attacker.example", http.StatusMisdirectedRequest},
	} {
		called = false
		r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/report", nil)
		r.Header.Set("X-Forwarded-Host", "127.0.0.1:8080")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want || called != (tc.want == http.StatusOK) {
			t.Fatalf("host %q: code=%d called=%v", tc.host, w.Code, called)
		}
	}
}
