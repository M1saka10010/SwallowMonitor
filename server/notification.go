package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/M1saka10010/SwallowMonitor/store"
)

const notificationTimeout = 5 * time.Second
const offlineNotificationDelay = 5 * time.Minute
const dnsResolveTimeout = 3 * time.Second

var notificationReservedRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func isPublicNotificationIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	// Only the allocated IPv6 global-unicast space is eligible; this also
	// excludes translation prefixes that can encode an internal IPv4 target.
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	for _, prefix := range notificationReservedRanges {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

type notificationLookup func(context.Context, string, string) ([]net.IP, error)
type notificationDial func(context.Context, string, string) (net.Conn, error)

func resolveNotificationIPs(ctx context.Context, host string, lookup notificationLookup) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, dnsResolveTimeout)
	defer cancel()
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("notification host resolution failed")
	}
	for _, ip := range ips {
		if !isPublicNotificationIP(ip) {
			return nil, errors.New("notification destination is not public")
		}
	}
	return ips, nil
}

func isPrivateHost(host string) bool {
	_, err := resolveNotificationIPs(context.Background(), host, net.DefaultResolver.LookupIP)
	return err != nil
}

func newNotificationTransport(lookup notificationLookup, dial notificationDial) *http.Transport {
	return &http.Transport{
		Proxy:               nil,
		TLSHandshakeTimeout: notificationTimeout,
		DisableKeepAlives:   true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("invalid notification address")
			}
			ips, err := resolveNotificationIPs(ctx, host, lookup)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
			}
			return nil, errors.New("notification connection failed")
		},
	}
}

var notificationTransport = newNotificationTransport(net.DefaultResolver.LookupIP, (&net.Dialer{Timeout: notificationTimeout}).DialContext)

func newNotificationClient() *http.Client {
	return &http.Client{
		Timeout:       notificationTimeout,
		Transport:     notificationTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func normalizeNotificationRule(rule store.NotificationRule) (store.NotificationRule, string) {
	rule.Tag = strings.TrimSpace(rule.Tag)
	rule.URL = strings.TrimSpace(rule.URL)
	if rule.URL == "" {
		return rule, "url required"
	}
	parsed, err := url.Parse(strings.ReplaceAll(rule.URL, "%text%", "notification"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return rule, "invalid url"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return rule, "url must use http or https"
	}
	if isPrivateHost(parsed.Hostname()) {
		return rule, "url must not target private or internal hosts"
	}
	if !strings.Contains(rule.URL, "%text%") {
		return rule, "url must contain %text%"
	}
	if !rule.NotifyOnline && !rule.NotifyOffline {
		return rule, "at least one event required"
	}
	return rule, ""
}

func (s *Server) notifyHostStatus(publicID string, online bool) {
	host, err := s.store.GetHost(publicID)
	if err != nil {
		s.debugf("notify: get host failed publicID=%s: %v", publicID, err)
		return
	}
	event := "offline"
	statusText := "离线"
	if online {
		event = "online"
		statusText = "上线"
	}
	rules, err := s.store.MatchingNotificationRules(host.Tags, event)
	if err != nil {
		s.debugf("notify: list rules failed: %v", err)
		return
	}
	if len(rules) == 0 {
		return
	}
	text := formatStatusNotificationText(host, statusText)
	seen := make(map[string]struct{}, len(rules))
	client := newNotificationClient()
	for _, rule := range rules {
		notifyURL := strings.ReplaceAll(rule.URL, "%text%", url.QueryEscape(text))
		if _, dup := seen[notifyURL]; dup {
			continue
		}
		seen[notifyURL] = struct{}{}
		s.sendNotificationGET(client, notifyURL)
	}
}

func (s *Server) scheduleOfflineNotification(publicID string) {
	s.scheduleOfflineNotificationAfter(publicID, offlineNotificationDelay, func() {
		s.notifyHostStatus(publicID, false)
	})
}

func (s *Server) scheduleOfflineNotificationAfter(publicID string, delay time.Duration, notify func()) {
	s.offlineNotificationMu.Lock()
	defer s.offlineNotificationMu.Unlock()

	if timer := s.offlineNotificationTimers[publicID]; timer != nil {
		timer.Stop()
	}

	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		s.offlineNotificationMu.Lock()
		if s.offlineNotificationTimers[publicID] != timer {
			s.offlineNotificationMu.Unlock()
			return
		}
		delete(s.offlineNotificationTimers, publicID)
		stillOffline := !s.hub.IsOnline(publicID)
		s.offlineNotificationMu.Unlock()

		if stillOffline {
			notify()
		}
	})
	s.offlineNotificationTimers[publicID] = timer
}

// cancelOfflineNotification stops a pending offline notification. It reports
// whether one was pending, i.e. the host went offline less than
// offlineNotificationDelay ago and no offline notification was sent.
func (s *Server) cancelOfflineNotification(publicID string) bool {
	s.offlineNotificationMu.Lock()
	defer s.offlineNotificationMu.Unlock()

	if timer := s.offlineNotificationTimers[publicID]; timer != nil {
		timer.Stop()
		delete(s.offlineNotificationTimers, publicID)
		return true
	}
	return false
}

func formatStatusNotificationText(host *store.Host, statusText string) string {
	parts := []string{fmt.Sprintf("主机「%s」已%s", host.Nickname, statusText)}
	if host.Hostname != "" {
		parts = append(parts, "Host: "+host.Hostname)
	}
	if len(host.Tags) > 0 {
		parts = append(parts, "标签: "+strings.Join(host.Tags, ", "))
	}
	return strings.Join(parts, "，")
}

func (s *Server) sendNotificationGET(client *http.Client, notifyURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), notificationTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, notifyURL, nil)
	if err != nil {
		s.debugf("notify: build request failed")
		return
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		s.debugf("notify: unsupported URL scheme")
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		s.debugf("notify: request failed")
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.debugf("notify: non-2xx status %d", resp.StatusCode)
	}
}
