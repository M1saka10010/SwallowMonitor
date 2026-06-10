package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "swallow_session"

type session struct {
	user    string
	expires time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
	states   map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		sessions: make(map[string]session),
		states:   make(map[string]time.Time),
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
	ss.sessions[id] = session{user: user, expires: time.Now().Add(7 * 24 * time.Hour)}
	ss.mu.Unlock()
	return id
}

func (ss *sessionStore) get(id string) (string, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
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
	delete(ss.sessions, id)
	ss.mu.Unlock()
}

func (ss *sessionStore) newState() string {
	st := randToken()
	ss.mu.Lock()
	ss.states[st] = time.Now().Add(10 * time.Minute)
	ss.mu.Unlock()
	return st
}

func (ss *sessionStore) consumeState(st string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	exp, ok := ss.states[st]
	if ok {
		delete(ss.states, st)
	}
	return ok && time.Now().Before(exp)
}

func (s *Server) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	return s.sessions.get(c.Value)
}

// canManage reports whether the requester may perform management operations
// (and thus see tokens). True when OAuth is disabled or the user is logged in.
func (s *Server) canManage(r *http.Request) bool {
	if s.oauth == nil {
		return true
	}
	_, ok := s.currentUser(r)
	return ok
}

// requireUser enforces an authenticated session for write operations. If GitHub
// OAuth is not configured, all requests are allowed. On failure it writes a 401
// and returns false.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) bool {
	if s.canManage(r) {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		http.Error(w, "github oauth not configured", http.StatusNotImplemented)
		return
	}
	state := s.sessions.newState()
	http.Redirect(w, r, s.oauth.AuthCodeURL(state), http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		http.Error(w, "github oauth not configured", http.StatusNotImplemented)
		return
	}
	if !s.sessions.consumeState(r.URL.Query().Get("state")) {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
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
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.cfg.PublicURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) userAllowed(login string) bool {
	allowed := s.cfg.GitHub.AllowedUsers
	if len(allowed) == 0 {
		return true
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

	var body struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Login, nil
}
