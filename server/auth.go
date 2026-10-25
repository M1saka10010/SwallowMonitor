package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie          = "swallow_session"
	stateCookie            = "swallow_oauth_state"
	secureSessionCookie    = "__Host-swallow_session"
	secureStateCookie      = "__Host-swallow_oauth_state"
	stateTTL               = 10 * time.Minute
	maxAuthEntries         = 4096
	loginAttemptsPerMinute = 10
)

type loginWindow struct {
	count   int
	expires time.Time
}

type session struct {
	user    string
	expires time.Time
}

type sessionStore struct {
	mu           sync.Mutex
	sessions     map[string]session
	states       map[string]time.Time
	loginWindows map[string]loginWindow
	nextCleanup  time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		sessions:     make(map[string]session),
		states:       make(map[string]time.Time),
		loginWindows: make(map[string]loginWindow),
	}
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (ss *sessionStore) create(user string) string {
	id := randToken()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.cleanupLocked(time.Now())
	if len(ss.sessions) >= maxAuthEntries {
		return ""
	}
	ss.sessions[id] = session{user: user, expires: time.Now().Add(7 * 24 * time.Hour)}
	return id
}

func (ss *sessionStore) get(id string) (string, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.cleanupLocked(time.Now())
	s, ok := ss.sessions[id]
	if !ok || time.Now().After(s.expires) {
		if ok {
			delete(ss.sessions, id)
		}
		return "", false
	}
	return s.user, true
}

func (ss *sessionStore) delete(id string) {
	ss.mu.Lock()
	ss.cleanupLocked(time.Now())
	delete(ss.sessions, id)
	ss.mu.Unlock()
}

func (ss *sessionStore) newState() string {
	st := randToken()
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.cleanupLocked(time.Now())
	if len(ss.states) >= maxAuthEntries {
		return ""
	}
	ss.states[st] = time.Now().Add(stateTTL)
	return st
}

func (ss *sessionStore) consumeState(st string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.cleanupLocked(time.Now())
	exp, ok := ss.states[st]
	if ok {
		delete(ss.states, st)
	}
	return ok && time.Now().Before(exp)
}

// Cleanup is lazy so stores need no background goroutine or shutdown hook.
func (ss *sessionStore) cleanupLocked(now time.Time) {
	if now.Before(ss.nextCleanup) {
		return
	}
	ss.nextCleanup = now.Add(time.Minute)
	for id, s := range ss.sessions {
		if !now.Before(s.expires) {
			delete(ss.sessions, id)
		}
	}
	for state, expires := range ss.states {
		if !now.Before(expires) {
			delete(ss.states, state)
		}
	}
	for ip, window := range ss.loginWindows {
		if !now.Before(window.expires) {
			delete(ss.loginWindows, ip)
		}
	}
}

func (ss *sessionStore) allowLogin(ip string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	now := time.Now()
	ss.cleanupLocked(now)
	window, ok := ss.loginWindows[ip]
	if ok && !now.Before(window.expires) {
		delete(ss.loginWindows, ip)
		ok = false
		window = loginWindow{}
	}
	if !ok {
		if len(ss.loginWindows) >= maxAuthEntries {
			return false
		}
		window.expires = now.Add(time.Minute)
	}
	if window.count >= loginAttemptsPerMinute {
		return false
	}
	window.count++
	ss.loginWindows[ip] = window
	return true
}

func (s *Server) secureCookies() bool {
	return strings.HasPrefix(s.cfg.PublicURL, "https://")
}

func (s *Server) cookieName(insecureName, secureName string) string {
	if s.secureCookies() {
		return secureName
	}
	return insecureName
}

func (s *Server) sessionCookieName() string {
	return s.cookieName(sessionCookie, secureSessionCookie)
}

func (s *Server) stateCookieName() string {
	return s.cookieName(stateCookie, secureStateCookie)
}

func (s *Server) setAuthCookie(w http.ResponseWriter, name, value string, maxAge int, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
		Expires: expires,
	})
}

func singleCookie(r *http.Request, name string) (*http.Cookie, bool) {
	var match *http.Cookie
	for _, cookie := range r.Cookies() {
		if cookie.Name != name {
			continue
		}
		if match != nil {
			return nil, false
		}
		match = cookie
	}
	return match, match != nil
}

func (s *Server) currentUser(r *http.Request) (string, bool) {
	c, ok := singleCookie(r, s.sessionCookieName())
	if !ok {
		return "", false
	}
	return s.sessions.get(c.Value)
}

// canManage reports whether the requester may perform management operations
// (and thus see device public keys). Without OAuth, explicit development mode is required.
func (s *Server) canManage(r *http.Request) bool {
	if s.oauth == nil {
		return s.cfg.InsecureDevelopment
	}
	_, ok := s.currentUser(r)
	return ok
}

// requireUser enforces an authenticated session for write operations. If GitHub
// OAuth is not configured, explicit development mode is required.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) bool {
	if s.canManage(r) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !s.managementSameOrigin(r) {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return false
		}
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.oauth == nil {
		http.Error(w, "github oauth not configured", http.StatusNotImplemented)
		return
	}
	if !s.sessions.allowLogin(s.clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many login attempts", http.StatusTooManyRequests)
		return
	}
	state := s.sessions.newState()
	if state == "" {
		http.Error(w, "authentication capacity reached", http.StatusServiceUnavailable)
		return
	}
	s.setAuthCookie(w, s.stateCookieName(), state, int(stateTTL/time.Second), time.Now().Add(stateTTL))
	http.Redirect(w, r, s.oauth.AuthCodeURL(state), http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.oauth == nil {
		http.Error(w, "github oauth not configured", http.StatusNotImplemented)
		return
	}
	state := r.URL.Query().Get("state")
	cookie, ok := singleCookie(r, s.stateCookieName())
	if !ok || state == "" || cookie.Value != state || !s.sessions.consumeState(state) {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	s.setAuthCookie(w, s.stateCookieName(), "", -1, time.Unix(1, 0))
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	tok, err := s.oauth.Exchange(ctx, code)
	if err != nil {
		s.debugf("auth: exchange failed: %v", err)
		http.Error(w, "oauth exchange failed", http.StatusBadGateway)
		return
	}

	login, err := s.fetchGitHubLogin(ctx, tok.AccessToken)
	if err != nil {
		s.debugf("auth: fetch user failed: %v", err)
		http.Error(w, "failed to fetch github user", http.StatusBadGateway)
		return
	}

	if !s.userAllowed(login) {
		http.Error(w, "user not allowed", http.StatusForbidden)
		return
	}

	id := s.sessions.create(login)
	if id == "" {
		http.Error(w, "authentication capacity reached", http.StatusServiceUnavailable)
		return
	}
	s.setAuthCookie(w, s.sessionCookieName(), id, 0, time.Now().Add(7*24*time.Hour))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.logoutSameOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	if c, ok := singleCookie(r, s.sessionCookieName()); ok {
		s.sessions.delete(c.Value)
	}
	s.setAuthCookie(w, s.sessionCookieName(), "", -1, time.Unix(1, 0))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logoutSameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	actual, err := url.Parse(origin)
	if err != nil || actual.User != nil {
		return false
	}
	expected, err := url.Parse(s.cfg.PublicURL)
	return err == nil && (expected.Scheme == "http" || expected.Scheme == "https") &&
		expected.Host != "" && actual.Scheme == expected.Scheme && strings.EqualFold(actual.Host, expected.Host)
}

func (s *Server) managementSameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	if r.Header.Get("Origin") == "" && r.Header.Get("Referer") == "" {
		return true
	}
	return s.logoutSameOrigin(r)
}

func (s *Server) userAllowed(login string) bool {
	allowed := s.cfg.GitHub.AllowedUsers
	if strings.TrimSpace(login) == "" {
		return false
	}
	for _, u := range allowed {
		if u == login {
			return true
		}
	}
	return false
}

func (s *Server) fetchGitHubLogin(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github user request returned status %d", resp.StatusCode)
	}

	var body struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if strings.TrimSpace(body.Login) == "" {
		return "", fmt.Errorf("github user response has empty login")
	}
	return body.Login, nil
}
