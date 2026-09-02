package server

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// cleanupThreshold is the map size at which Allow lazily purges stale IPs.
const cleanupThreshold = 1000

// ipRateLimiter is a per-IP sliding-window rate limiter. Each IP may make
// at most max requests within window. It is safe for concurrent use.
type ipRateLimiter struct {
	mu     sync.Mutex
	counts map[string][]time.Time
	max    int
	window time.Duration
}

// newIPRateLimiter creates a limiter that allows max requests per window.
func newIPRateLimiter(max int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		counts: make(map[string][]time.Time),
		max:    max,
		window: window,
	}
}

// Allow reports whether ip is within the limit. When allowed, the attempt is
// recorded. Expired timestamps for the calling IP are trimmed on every call;
// when the map grows past cleanupThreshold all stale IPs are purged.
func (rl *ipRateLimiter) Allow(ip string) bool {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.counts) > cleanupThreshold {
		rl.cleanupLocked(cutoff)
	}

	ts := rl.counts[ip]
	start := 0
	for start < len(ts) && ts[start].Before(cutoff) {
		start++
	}
	ts = ts[start:]

	if len(ts) >= rl.max {
		rl.counts[ip] = ts
		return false
	}
	rl.counts[ip] = append(ts, now)
	return true
}

// cleanupLocked removes IPs whose every timestamp has expired. Caller must
// hold the lock.
func (rl *ipRateLimiter) cleanupLocked(cutoff time.Time) {
	for ip, ts := range rl.counts {
		start := 0
		for start < len(ts) && ts[start].Before(cutoff) {
			start++
		}
		if start >= len(ts) {
			delete(rl.counts, ip)
		} else {
			rl.counts[ip] = ts[start:]
		}
	}
}

// clientIP determines the real client IP for a request.
//
// When the request comes from a non-trusted source (direct connection or an
// unknown proxy), the TCP RemoteAddr is used. When it comes from a trusted
// proxy, X-Forwarded-For is walked right-to-left past trusted proxy IPs; the
// first untrusted entry is the real client. This prevents a client from
// forging X-Forwarded-For to bypass rate limiting — the header is only
// honoured when the connection itself originates from a trusted address.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !s.isTrustedProxy(host) {
		return host
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip != "" && !s.isTrustedProxy(ip) {
			return ip
		}
	}
	return strings.TrimSpace(parts[0])
}

// isTrustedProxy reports whether ip matches any configured trusted-proxy CIDR.
func (s *Server) isTrustedProxy(ip string) bool {
	if len(s.trustedProxies) == 0 {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, cidr := range s.trustedProxies {
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}

// parseTrustedProxies converts a list of IPs and/or CIDR strings into IPNets.
// Single IPs are treated as /32 (IPv4) or /128 (IPv6). Invalid entries are
// logged and skipped so the server can still start.
func parseTrustedProxies(proxies []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, p := range proxies {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			ip := net.ParseIP(p)
			if ip == nil {
				log.Printf("trustedProxies: skipping invalid entry %q", p)
				continue
			}
			if ip.To4() != nil {
				p += "/32"
			} else {
				p += "/128"
			}
		}
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			log.Printf("trustedProxies: skipping invalid CIDR %q: %v", p, err)
			continue
		}
		nets = append(nets, n)
	}
	return nets
}
