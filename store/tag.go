package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Tag is a reusable host label managed independently from hosts.
type Tag struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

func scanTag(sc interface{ Scan(...any) error }) (Tag, error) {
	var tag Tag
	err := sc.Scan(&tag.ID, &tag.Name, &tag.CreatedAt)
	return tag, err
}

// ListTags returns all tags ordered by name.
func (s *Store) ListTags() ([]Tag, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := []Tag{}
	for rows.Next() {
		tag, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// CreateTag creates a reusable host tag.
func (s *Store) CreateTag(name string) (Tag, error) {
	name = strings.TrimSpace(name)
	tag := Tag{Name: name, CreatedAt: time.Now().Unix()}
	res, err := s.db.Exec(`INSERT INTO tags (name, created_at) VALUES (?, ?)`, tag.Name, tag.CreatedAt)
	if err != nil {
		return tag, err
	}
	tag.ID, err = res.LastInsertId()
	return tag, err
}

// GetTag returns a tag by id.
func (s *Store) GetTag(id int64) (Tag, error) {
	tag, err := scanTag(s.db.QueryRow(`SELECT id, name, created_at FROM tags WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return tag, ErrNotFound
	}
	return tag, err
}

// UpdateTag renames a tag; host associations remain attached by id.
func (s *Store) UpdateTag(id int64, name string) error {
	res, err := s.db.Exec(`UPDATE tags SET name = ? WHERE id = ?`, strings.TrimSpace(name), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTag deletes a tag and all host associations.
func (s *Store) DeleteTag(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM host_tags WHERE tag_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) tagIDsByNames(tags []string) (map[string]int64, error) {
	cleaned := uniqueTrimmed(tags)
	ids := make(map[string]int64, len(cleaned))
	if len(cleaned) == 0 {
		return ids, nil
	}
	rows, err := s.db.Query(`SELECT id, name FROM tags`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	wanted := make(map[string]struct{}, len(cleaned))
	for _, tag := range cleaned {
		wanted[tag] = struct{}{}
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if _, ok := wanted[name]; ok {
			ids[name] = id
		}
	}
	return ids, rows.Err()
}

func uniqueTrimmed(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func (s *Store) replaceHostTags(publicID string, tags []string) error {
	ids, err := s.tagIDsByNames(tags)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM host_tags WHERE public_id = ?`, publicID); err != nil {
		return err
	}
	for _, tag := range uniqueTrimmed(tags) {
		id, ok := ids[tag]
		if !ok {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO host_tags(public_id, tag_id) VALUES(?, ?)`, publicID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) existingTagNames(tags []string) ([]string, error) {
	ids, err := s.tagIDsByNames(tags)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, tag := range uniqueTrimmed(tags) {
		if _, ok := ids[tag]; ok {
			out = append(out, tag)
		}
	}
	return out, nil
}

func (s *Store) loadTagsForHosts(hosts []*Host) error {
	if len(hosts) == 0 {
		return nil
	}
	byID := make(map[string]*Host, len(hosts))
	for _, h := range hosts {
		h.Tags = []string{}
		byID[h.PublicID] = h
	}
	rows, err := s.db.Query(`SELECT ht.public_id, t.name FROM host_tags ht JOIN tags t ON t.id = ht.tag_id ORDER BY t.name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var publicID, name string
		if err := rows.Scan(&publicID, &name); err != nil {
			return err
		}
		if h := byID[publicID]; h != nil {
			h.Tags = append(h.Tags, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateHostTags() error {
	type legacyHostTags struct {
		publicID string
		tags     []string
	}
	legacy := []legacyHostTags{}

	rows, err := s.db.Query(`SELECT public_id, COALESCE(tags, '[]') FROM hosts WHERE COALESCE(tags, '') != ''`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var publicID, raw string
		if err := rows.Scan(&publicID, &raw); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, legacyHostTags{publicID: publicID, tags: decodeTags(raw)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, item := range legacy {
		for _, name := range item.tags {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			res, err := s.db.Exec(`INSERT OR IGNORE INTO tags(name, created_at) VALUES(?, ?)`, name, time.Now().Unix())
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			if id == 0 {
				if err := s.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id); err != nil {
					return err
				}
			}
			if _, err := s.db.Exec(`INSERT OR IGNORE INTO host_tags(public_id, tag_id) VALUES(?, ?)`, item.publicID, id); err != nil {
				return err
			}
		}
	}
	_, err = s.db.Exec(`UPDATE hosts SET tags = '[]' WHERE COALESCE(tags, '') != ''`)
	return err
}
