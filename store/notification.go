package store

import (
	"database/sql"
	"errors"
	"time"
)

// NotificationRule maps a host tag to a GET notification URL.
type NotificationRule struct {
	ID            int64  `json:"id"`
	Tag           string `json:"tag"`
	URL           string `json:"url"`
	NotifyOnline  bool   `json:"notifyOnline"`
	NotifyOffline bool   `json:"notifyOffline"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     int64  `json:"createdAt"`
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func scanNotificationRule(sc interface{ Scan(...any) error }) (NotificationRule, error) {
	var r NotificationRule
	var notifyOnline, notifyOffline, enabled int
	err := sc.Scan(&r.ID, &r.Tag, &r.URL, &notifyOnline, &notifyOffline, &enabled, &r.CreatedAt)
	r.NotifyOnline = notifyOnline != 0
	r.NotifyOffline = notifyOffline != 0
	r.Enabled = enabled != 0
	return r, err
}

// ListNotificationRules returns all configured notification rules.
func (s *Store) ListNotificationRules() ([]NotificationRule, error) {
	rows, err := s.db.Query(`SELECT id, tag, url, notify_online, notify_offline, enabled, created_at FROM notification_rules ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []NotificationRule{}
	for rows.Next() {
		r, err := scanNotificationRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// CreateNotificationRule inserts a notification rule.
func (s *Store) CreateNotificationRule(rule NotificationRule) (NotificationRule, error) {
	rule.CreatedAt = time.Now().Unix()
	res, err := s.db.Exec(`INSERT INTO notification_rules (tag, url, notify_online, notify_offline, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		rule.Tag, rule.URL, boolInt(rule.NotifyOnline), boolInt(rule.NotifyOffline), boolInt(rule.Enabled), rule.CreatedAt)
	if err != nil {
		return rule, err
	}
	rule.ID, err = res.LastInsertId()
	return rule, err
}

// UpdateNotificationRule updates an existing notification rule.
func (s *Store) UpdateNotificationRule(rule NotificationRule) error {
	res, err := s.db.Exec(`UPDATE notification_rules SET tag = ?, url = ?, notify_online = ?, notify_offline = ?, enabled = ? WHERE id = ?`,
		rule.Tag, rule.URL, boolInt(rule.NotifyOnline), boolInt(rule.NotifyOffline), boolInt(rule.Enabled), rule.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteNotificationRule removes a notification rule.
func (s *Store) DeleteNotificationRule(id int64) error {
	res, err := s.db.Exec(`DELETE FROM notification_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MatchingNotificationRules returns enabled rules matching tags and event.
func (s *Store) MatchingNotificationRules(tags []string, event string) ([]NotificationRule, error) {
	rules, err := s.ListNotificationRules()
	if err != nil {
		return nil, err
	}
	tagSet := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tagSet[tag] = struct{}{}
	}

	matched := []NotificationRule{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if event == "online" && !rule.NotifyOnline {
			continue
		}
		if event == "offline" && !rule.NotifyOffline {
			continue
		}
		if rule.Tag != "" {
			if _, ok := tagSet[rule.Tag]; !ok {
				continue
			}
		}
		matched = append(matched, rule)
	}
	return matched, nil
}

// GetNotificationRule returns one notification rule by id.
func (s *Store) GetNotificationRule(id int64) (NotificationRule, error) {
	rule, err := scanNotificationRule(s.db.QueryRow(`SELECT id, tag, url, notify_online, notify_offline, enabled, created_at FROM notification_rules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return rule, ErrNotFound
	}
	return rule, err
}
