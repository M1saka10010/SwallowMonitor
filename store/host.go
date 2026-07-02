package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
)

// ErrNotFound is returned when a host does not exist.
var ErrNotFound = errors.New("host not found")

// Host represents a managed host row.
type Host struct {
	PublicID           string   `json:"publicId"`
	Token              string   `json:"token,omitempty"`
	Nickname           string   `json:"nickname"`
	Tags               []string `json:"tags"`
	HostID             string   `json:"hostId"`
	Hostname           string   `json:"hostname"`
	OS                 string   `json:"os"`
	Platform           string   `json:"platform"`
	PlatformVersion    string   `json:"platformVersion"`
	KernelArch         string   `json:"kernelArch"`
	ModelName          string   `json:"modelName"`
	Cores              int32    `json:"cores"`
	VirtualizationRole string   `json:"virtualizationRole"`
	BootTime           uint64   `json:"bootTime"`
	LastInfoJSON       string   `json:"-"`
	LastSeen           int64    `json:"lastSeen"`
	CreatedAt          int64    `json:"createdAt"`
}

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeTags(raw string) []string {
	tags := []string{}
	if raw == "" {
		return tags
	}
	_ = json.Unmarshal([]byte(raw), &tags)
	if tags == nil {
		tags = []string{}
	}
	return tags
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateHost inserts a new host. If token is empty, one is generated.
// Returns the created host including its token.
func (s *Store) CreateHost(nickname, token string, tags []string) (*Host, error) {
	existingTags, err := s.existingTagNames(tags)
	if err != nil {
		return nil, err
	}
	publicID, err := randomHex(8) // 16 hex chars
	if err != nil {
		return nil, err
	}
	if token == "" {
		token, err = randomHex(16) // 32 hex chars
		if err != nil {
			return nil, err
		}
	}
	now := time.Now().Unix()
	_, err = s.db.Exec(
		`INSERT INTO hosts (public_id, token, nickname, tags, created_at) VALUES (?, ?, ?, ?, ?)`,
		publicID, token, nickname, encodeTags(nil), now,
	)
	if err != nil {
		return nil, err
	}
	if err := s.replaceHostTags(publicID, existingTags); err != nil {
		return nil, err
	}
	return &Host{PublicID: publicID, Token: token, Nickname: nickname, Tags: existingTags, CreatedAt: now}, nil
}

// UpdateHost updates a host's nickname and tags by public id.
func (s *Store) UpdateHost(publicID, nickname string, tags []string) error {
	existingTags, err := s.existingTagNames(tags)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE hosts SET nickname = ?, tags = ? WHERE public_id = ?`,
		nickname, encodeTags(nil), publicID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return s.replaceHostTags(publicID, existingTags)
}

// DeleteHost removes a host and its usage history.
func (s *Store) DeleteHost(publicID string) error {
	res, err := s.db.Exec(`DELETE FROM hosts WHERE public_id = ?`, publicID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_, _ = s.db.Exec(`DELETE FROM usages WHERE public_id = ?`, publicID)
	_, _ = s.db.Exec(`DELETE FROM host_tags WHERE public_id = ?`, publicID)
	return nil
}

const hostCols = `public_id, token, nickname, COALESCE(tags,'[]'), COALESCE(host_id,''), COALESCE(hostname,''),
	COALESCE(os,''), COALESCE(platform,''), COALESCE(platform_version,''),
	COALESCE(kernel_arch,''), COALESCE(model_name,''), COALESCE(cores,0),
	COALESCE(virtualization_role,''), COALESCE(boot_time,0),
	COALESCE(last_seen,0), COALESCE(created_at,0)`

func scanHost(sc interface{ Scan(...any) error }) (*Host, error) {
	h := &Host{}
	var tagsRaw string
	err := sc.Scan(&h.PublicID, &h.Token, &h.Nickname, &tagsRaw, &h.HostID, &h.Hostname, &h.OS,
		&h.Platform, &h.PlatformVersion, &h.KernelArch, &h.ModelName, &h.Cores,
		&h.VirtualizationRole, &h.BootTime, &h.LastSeen, &h.CreatedAt)
	if err != nil {
		return nil, err
	}
	h.Tags = decodeTags(tagsRaw)
	return h, nil
}

// ListHosts returns all hosts. The token is included; callers must strip it for
// unauthenticated responses.
func (s *Store) ListHosts() ([]*Host, error) {
	rows, err := s.db.Query(`SELECT ` + hostCols + ` FROM hosts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []*Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hosts, s.loadTagsForHosts(hosts)
}

// GetHost returns a single host by public id. The token is included; callers
// must strip it for unauthenticated responses.
func (s *Store) GetHost(publicID string) (*Host, error) {
	row := s.db.QueryRow(`SELECT `+hostCols+` FROM hosts WHERE public_id = ?`, publicID)
	h, err := scanHost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadTagsForHosts([]*Host{h}); err != nil {
		return nil, err
	}
	return h, nil
}

// PublicIDByToken resolves a public id from an agent token. Returns ErrNotFound
// if the token is not registered.
func (s *Store) PublicIDByToken(token string) (string, error) {
	var publicID string
	err := s.db.QueryRow(`SELECT public_id FROM hosts WHERE token = ?`, token).Scan(&publicID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return publicID, err
}

// UpdateInfo stores the latest system_info snapshot for a host.
func (s *Store) UpdateInfo(publicID string, info *model.SystemInfo, infoJSON string) error {
	_, err := s.db.Exec(`UPDATE hosts SET
		host_id = ?, hostname = ?, os = ?, platform = ?, platform_version = ?,
		kernel_arch = ?, model_name = ?, cores = ?, virtualization_role = ?,
		boot_time = ?, last_info_json = ?, last_seen = ?
		WHERE public_id = ?`,
		info.HostID, info.Hostname, info.OS, info.Platform, info.PlatformVersion,
		info.KernelArch, info.ModelName, info.Cores, info.VirtualizationRole,
		info.BootTime, infoJSON, time.Now().Unix(), publicID,
	)
	return err
}

// Touch updates last_seen for a host.
func (s *Store) Touch(publicID string, ts int64) error {
	_, err := s.db.Exec(`UPDATE hosts SET last_seen = ? WHERE public_id = ?`, ts, publicID)
	return err
}
