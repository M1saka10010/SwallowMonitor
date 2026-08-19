package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/M1saka10010/SwallowMonitor/store"
)

const notificationTimeout = 5 * time.Second
const offlineNotificationDelay = 5 * time.Minute
const dnsResolveTimeout = 3 * time.Second

// isPrivateHost resolves host and returns true when any IP belongs to a
// private, link-local, or otherwise non-public address range. Loopback is
// allowed for development and testing.
func isPrivateHost(host string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), dnsResolveTimeout)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return true // fail closed: reject unresolvable hosts
	}
	for _, ip := range ips {
		if ip.IsLoopback() {
			continue
		}
		if ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() ||
			ip.IsInterfaceLocalMulticast() {
			return true
		}
	}
	return false
}

func normalizeNotificationRule(rule store.NotificationRule) (store.NotificationRule, string) {
	rule.Tag = strings.TrimSpace(rule.Tag)
	rule.URL = strings.TrimSpace(rule.URL)
	if rule.URL == "" {
		return rule, "url required"
	}
	parsed, err := url.Parse(rule.URL)
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
	client := &http.Client{Timeout: notificationTimeout}
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
		s.debugf("notify: build request failed: %v", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		s.debugf("notify: request failed: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.debugf("notify: non-2xx status %d", resp.StatusCode)
	}
}
