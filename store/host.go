package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/M1saka10010/SwallowMonitor/model"
)

// ErrNotFound is returned when a host does not exist.
var ErrNotFound = errors.New("host not found")
var ErrEnrollmentInvalid = errors.New("invalid enrollment")
var ErrAlreadyBound = errors.New("host already bound")

// Host represents a managed host row.
type Host struct {
	AgentVersion       string   `json:"agentVersion"`
	PublicID           string   `json:"publicId"`
	PublicKey          string   `json:"publicKey,omitempty"`
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

func validPublicKey(publicKey string) bool {
	if len(publicKey) != base64.StdEncoding.EncodedLen(ed25519.PublicKeySize) {
		return false
	}
	key, err := base64.StdEncoding.Strict().DecodeString(publicKey)
	return err == nil && len(key) == ed25519.PublicKeySize && base64.StdEncoding.EncodeToString(key) == publicKey
}

// CreateHost creates a pending host when publicKey is empty, or binds a key.
func (s *Store) CreateHost(nickname, publicKey string, tags []string) (*Host, error) {
	return s.createHost(nickname, publicKey, tags, nil)
}

func (s *Store) createHost(nickname, publicKey string, tags []string, enrollment *Enrollment) (*Host, error) {
	if len(nickname) > 400 {
		return nil, fmt.Errorf("host fields too long")
	}
	if publicKey != "" && !validPublicKey(publicKey) {
		return nil, fmt.Errorf("publicKey must be canonical base64 encoding of a 32-byte Ed25519 public key")
	}
	existingTags, err := s.existingTagNames(tags)
	if err != nil {
		return nil, err
	}
	publicID, err := randomHex(8) // 16 hex chars
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	query := "INSERT INTO hosts (public_id, public_key, nickname, tags, created_at) VALUES (?, ?, ?, ?, ?)"
	var storedKey any
	if publicKey != "" {
		storedKey = publicKey
	}
	args := []any{publicID, storedKey, nickname, encodeTags(nil), now}
	if s.legacyTokenColumn {
		// The obsolete NOT NULL column is never used as an identity.
		placeholder, err := randomHex(32)
		if err != nil {
			return nil, err
		}
		query = "INSERT INTO hosts (public_id, public_key, nickname, tags, created_at, token) VALUES (?, ?, ?, ?, ?, ?)"
		args = append(args, placeholder)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(query, args...)
	if err != nil {
		return nil, err
	}
	for _, tag := range existingTags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO host_tags(public_id, tag_id) SELECT ?, id FROM tags WHERE name = ?`, publicID, tag); err != nil {
			return nil, err
		}
	}
	if enrollment != nil {
		sum := sha256.Sum256([]byte(enrollment.Token))
		if _, err := tx.Exec(`INSERT INTO host_enrollments(public_id, token_hash, expires_at, consumed) VALUES(?,?,?,0)`, publicID, sum[:], enrollment.ExpiresAt); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Host{PublicID: publicID, PublicKey: publicKey, Nickname: nickname, Tags: existingTags, CreatedAt: now}, nil
}

type Enrollment struct {
	Host      *Host
	Token     string
	ExpiresAt int64
}

func (s *Store) CreateEnrollmentHost(nickname string, tags []string) (*Enrollment, error) {
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	e := &Enrollment{Token: token, ExpiresAt: time.Now().Add(15 * time.Minute).Unix()}
	h, err := s.createHost(nickname, "", tags, e)
	if err != nil {
		return nil, err
	}
	e.Host = h
	return e, nil
}

func (s *Store) RefreshEnrollment(publicID string) (*Enrollment, error) {
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(token))
	exp := time.Now().Add(15 * time.Minute).Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var key sql.NullString
	if err = tx.QueryRow(`SELECT public_key FROM hosts WHERE public_id = ?`, publicID).Scan(&key); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if key.Valid && key.String != "" {
		return nil, ErrAlreadyBound
	}
	if _, err = tx.Exec(`DELETE FROM host_enrollments WHERE public_id = ?`, publicID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`INSERT INTO host_enrollments(public_id,token_hash,expires_at,consumed) VALUES(?,?,?,0)`, publicID, sum[:], exp); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	h, err := s.GetHost(publicID)
	if err != nil {
		return nil, err
	}
	return &Enrollment{Host: h, Token: token, ExpiresAt: exp}, nil
}

func (s *Store) ConsumeEnrollment(token, publicKey string, now int64) (string, error) {
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != token || !validPublicKey(publicKey) {
		return "", ErrEnrollmentInvalid
	}
	sum := sha256.Sum256([]byte(token))
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var publicID string
	var exp int64
	var consumed int
	err = tx.QueryRow(`SELECT public_id,expires_at,consumed FROM host_enrollments WHERE token_hash = ?`, sum[:]).Scan(&publicID, &exp, &consumed)
	if err != nil || consumed != 0 || exp <= now {
		return "", ErrEnrollmentInvalid
	}
	var key sql.NullString
	if err = tx.QueryRow(`SELECT public_key FROM hosts WHERE public_id=?`, publicID).Scan(&key); err != nil || (key.Valid && key.String != "") {
		return "", ErrEnrollmentInvalid
	}
	res, err := tx.Exec(`UPDATE hosts SET public_key=? WHERE public_id=? AND (public_key IS NULL OR public_key='')`, publicKey, publicID)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return "", ErrEnrollmentInvalid
	}
	res, err = tx.Exec(`UPDATE host_enrollments SET consumed=1 WHERE token_hash=? AND consumed=0`, sum[:])
	if err != nil {
		return "", err
	}
	n, _ = res.RowsAffected()
	if n != 1 {
		return "", ErrEnrollmentInvalid
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return publicID, nil
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
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM hosts WHERE public_id = ?`, publicID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	for _, table := range []string{"usages", "usages_5m", "usages_1h", "host_tags", "host_enrollments"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE public_id = ?", publicID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const hostCols = `public_id, COALESCE(public_key,''), nickname, COALESCE(tags,'[]'), COALESCE(host_id,''), COALESCE(hostname,''),
	COALESCE(os,''), COALESCE(platform,''), COALESCE(platform_version,''),
	COALESCE(kernel_arch,''), COALESCE(model_name,''), COALESCE(cores,0),
	COALESCE(virtualization_role,''), COALESCE(boot_time,0),
	COALESCE(last_seen,0), COALESCE(created_at,0), COALESCE(last_info_json,'')`

func scanHost(sc interface{ Scan(...any) error }) (*Host, error) {
	h := &Host{}
	var tagsRaw string
	err := sc.Scan(&h.PublicID, &h.PublicKey, &h.Nickname, &tagsRaw, &h.HostID, &h.Hostname, &h.OS,
		&h.Platform, &h.PlatformVersion, &h.KernelArch, &h.ModelName, &h.Cores,
		&h.VirtualizationRole, &h.BootTime, &h.LastSeen, &h.CreatedAt, &h.LastInfoJSON)
	if err != nil {
		return nil, err
	}
	h.Tags = decodeTags(tagsRaw)
	var info struct {
		AgentVersion string `json:"agentVersion"`
	}
	if json.Unmarshal([]byte(h.LastInfoJSON), &info) == nil {
		h.AgentVersion = info.AgentVersion
	}
	return h, nil
}

// ListHosts returns all hosts. The public key is included; callers must strip it for
// unauthenticated responses.
func (s *Store) ListHosts() ([]*Host, error) {
	return s.ListHostsContext(context.Background(), 0)
}

// ListHostsContext returns hosts ordered by creation time. A positive limit
// bounds the number of returned hosts.
func (s *Store) ListHostsContext(ctx context.Context, limit int) ([]*Host, error) {
	query := `SELECT ` + hostCols + ` FROM hosts ORDER BY created_at`
	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		rows, err = s.db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query)
	}
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
	return hosts, s.loadTagsForHostsContext(ctx, hosts)
}

// ListHostsWithLatestUsageContext returns a bounded host list and each host's
// most recent usage sample without issuing one query per host.
func (s *Store) ListHostsWithLatestUsageContext(ctx context.Context, limit int) ([]*Host, map[string]*model.SystemUsage, error) {
	hosts, err := s.ListHostsContext(ctx, limit)
	if err != nil {
		return nil, nil, err
	}
	latest, err := s.latestUsageForHostsContext(ctx, hosts)
	if err != nil {
		return nil, nil, err
	}
	return hosts, latest, nil
}

func (s *Store) latestUsageForHostsContext(ctx context.Context, hosts []*Host) (map[string]*model.SystemUsage, error) {
	latest := make(map[string]*model.SystemUsage, len(hosts))
	if len(hosts) == 0 {
		return latest, nil
	}

	args := make([]any, len(hosts))
	placeholders := make([]string, len(hosts))
	for i, h := range hosts {
		args[i] = h.PublicID
		placeholders[i] = "?"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.public_id, `+usageCols+` FROM usages u
		WHERE u.public_id IN (`+strings.Join(placeholders, ",")+`)
		AND u.id = (SELECT id FROM usages WHERE public_id = u.public_id ORDER BY ts DESC, id DESC LIMIT 1)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var publicID string
		u := &model.SystemUsage{}
		if err := rows.Scan(&publicID, &u.Timestamp, &u.CPUUsage, &u.MemoryTotal, &u.MemoryUsed,
			&u.SwapTotal, &u.SwapUsed, &u.DiskTotal, &u.DiskUsed, &u.NetRecv, &u.NetSend,
			&u.NetRecvSpeed, &u.NetSendSpeed, &u.Load1, &u.Load5, &u.Load15); err != nil {
			return nil, err
		}
		latest[publicID] = u
	}
	return latest, rows.Err()
}

func (s *Store) LatestUsageContext(ctx context.Context, publicID string) (*model.SystemUsage, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+usageCols+` FROM usages WHERE public_id = ? ORDER BY ts DESC LIMIT 1`, publicID)
	u, err := scanUsage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

// GetHost returns a single host by public id. The public key is included; callers
// must strip it for unauthenticated responses.
func (s *Store) GetHost(publicID string) (*Host, error) {
	return s.GetHostContext(context.Background(), publicID)
}

func (s *Store) GetHostContext(ctx context.Context, publicID string) (*Host, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+hostCols+` FROM hosts WHERE public_id = ?`, publicID)
	h, err := scanHost(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadTagsForHostContext(ctx, h); err != nil {
		return nil, err
	}
	return h, nil
}

// PublicIDByPublicKey resolves only approved, canonical public keys.
func (s *Store) PublicIDByPublicKey(publicKey string) (string, error) {
	if !validPublicKey(publicKey) {
		return "", ErrNotFound
	}
	var publicID string
	err := s.db.QueryRow(`SELECT public_id FROM hosts WHERE public_key = ?`, publicKey).Scan(&publicID)
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
