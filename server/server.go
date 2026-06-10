package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/M1saka10010/SwallowMonitor/model"
	"github.com/M1saka10010/SwallowMonitor/store"
	"github.com/gorilla/websocket"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// Server holds shared dependencies for HTTP handlers.
type Server struct {
	cfg      *model.Config
	store    *store.Store
	hub      *Hub
	sessions *sessionStore
	oauth    *oauth2.Config
	upgrader websocket.Upgrader
}

// New creates a Server and wires the HTTP routes onto mux.
func New(cfg *model.Config, st *store.Store, mux *http.ServeMux, webHandler http.Handler) *Server {
	s := &Server{
		cfg:      cfg,
		store:    st,
		hub:      NewHub(),
		sessions: newSessionStore(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	if cfg.GitHub.ClientID != "" {
		s.oauth = &oauth2.Config{
			ClientID:     cfg.GitHub.ClientID,
			ClientSecret: cfg.GitHub.ClientSecret,
			Endpoint:     github.Endpoint,
			RedirectURL:  cfg.PublicURL + "/auth/github/callback",
			Scopes:       []string{"read:user"},
		}
	}

	// Agent ingest (token auth, no session).
	mux.HandleFunc("/report", s.handleReport)

	// Auth endpoints.
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/auth/github/callback", s.handleCallback)
	mux.HandleFunc("/logout", s.handleLogout)

	// Public read-only endpoints: overview/detail data and live SSE stream.
	// Write operations on /api/hosts are guarded inside the handlers.
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/api/", s.handleAPI)

	// Static web panel (public; client hides management UI when not logged in).
	mux.Handle("/", webHandler)

	return s
}

func (s *Server) debugf(format string, args ...any) {
	if s.cfg.IsDebug {
		log.Printf(format, args...)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
