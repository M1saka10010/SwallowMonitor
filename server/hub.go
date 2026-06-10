package server

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub tracks live agent connections and browser overview subscribers.
type Hub struct {
	mu sync.RWMutex
	// agents maps public_id -> set of agent connections (usually one).
	agents map[string]map[*websocket.Conn]struct{}
	// overview holds subscribers that receive events for all hosts.
	overview map[*subscriber]struct{}
}

type subscriber struct {
	ch chan []byte
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{
		agents:   make(map[string]map[*websocket.Conn]struct{}),
		overview: make(map[*subscriber]struct{}),
	}
}

// AddAgent registers a live agent connection for a host. It reports whether
// this connection transitioned the host from offline to online.
func (h *Hub) AddAgent(publicID string, c *websocket.Conn) (becameOnline bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.agents[publicID] == nil {
		h.agents[publicID] = make(map[*websocket.Conn]struct{})
	}
	becameOnline = len(h.agents[publicID]) == 0
	h.agents[publicID][c] = struct{}{}
	return becameOnline
}

// RemoveAgent unregisters an agent connection. It reports whether the host
// transitioned to offline (no remaining connections).
func (h *Hub) RemoveAgent(publicID string, c *websocket.Conn) (becameOffline bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conns := h.agents[publicID]; conns != nil {
		delete(conns, c)
		if len(conns) == 0 {
			delete(h.agents, publicID)
			return true
		}
	}
	return false
}

// IsOnline reports whether a host has a live agent connection.
func (h *Hub) IsOnline(publicID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.agents[publicID]) > 0
}

// SubscribeOverview registers a subscriber that receives events for all hosts.
func (h *Hub) SubscribeOverview() *subscriber {
	sub := &subscriber{ch: make(chan []byte, 64)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.overview[sub] = struct{}{}
	return sub
}

// UnsubscribeOverview removes an overview subscriber.
func (h *Hub) UnsubscribeOverview(sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.overview, sub)
	close(sub.ch)
}

// PublishOverview broadcasts an event to all overview subscribers.
func (h *Hub) PublishOverview(payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.overview {
		select {
		case sub.ch <- data:
		default: // drop if subscriber is slow
		}
	}
}
