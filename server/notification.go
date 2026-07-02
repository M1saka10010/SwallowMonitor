package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/M1saka10010/SwallowMonitor/store"
)

const notificationTimeout = 5 * time.Second

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
