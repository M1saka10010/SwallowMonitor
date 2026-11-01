package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

type MaintenanceTask struct {
	Cursor        int64  `json:"-"`
	ID            string `json:"id"`
	PublicID      string `json:"publicId"`
	Kind          string `json:"kind"`
	TargetVersion string `json:"targetVersion,omitempty"`
	Status        string `json:"status"`
	CreatedBy     string `json:"createdBy"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
	LastSeq       int64  `json:"lastSeq"`
}
type MaintenanceEvent struct {
	TaskID    string `json:"taskId"`
	Seq       int64  `json:"seq"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"createdAt"`
}

var ErrMaintenanceConflict = errors.New("maintenance conflict")
var ErrMaintenanceGap = errors.New("maintenance sequence gap")
var maintenanceStatuses = map[string]bool{"accepted": true, "running": true, "verifying": true, "succeeded": true, "failed": true, "unknown": true}
var terminalMaintenance = map[string]bool{"succeeded": true, "failed": true}

// Expire only jobs never offered to an agent. A lost delivery is uncertain,
// not proof that execution did not start.
func (s *Store) ExpireMaintenance() error {
	now := time.Now().Unix()
	_, err := s.db.Exec("UPDATE maintenance_tasks SET status='expired',updated_at=? WHERE status='queued' AND delivered=0 AND created_at<?", now, now-60)
	return err
}

// PendingMaintenance persists the delivery intent before a network write.
// Retry the same ID until acceptance is recorded; the agent durably deduplicates.
func (s *Store) PendingMaintenance(publicID string) (*MaintenanceTask, error) {
	if err := s.ExpireMaintenance(); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRow("SELECT id FROM maintenance_tasks WHERE public_id=? AND status IN ('queued','delivered') AND last_seq=0", publicID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec("UPDATE maintenance_tasks SET delivered=1,status='delivered',updated_at=? WHERE id=?", time.Now().Unix(), id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMaintenance(id)
}

func validMaintenanceTransition(old, next string) bool {
	if !maintenanceStatuses[next] {
		return false
	}
	if old == "queued" || old == "delivered" {
		return next == "accepted"
	}
	if terminalMaintenance[old] || old == "expired" {
		return false
	}
	if next == "unknown" {
		return true
	}
	if old == "unknown" {
		return next == "running" || next == "verifying" || next == "succeeded" || next == "failed"
	}
	if terminalMaintenance[old] {
		return false
	}
	rank := map[string]int{"accepted": 1, "running": 2, "verifying": 3, "succeeded": 4, "failed": 4}
	return rank[next] >= rank[old]
}
func (s *Store) CreateMaintenance(publicID, kind, target, by string, now int64) (*MaintenanceTask, error) {
	id, e := randomHex(16)
	if e != nil {
		return nil, e
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRow("SELECT count(*) FROM maintenance_tasks WHERE public_id=? AND status NOT IN ('succeeded','failed','expired')", publicID).Scan(&exists); e != nil {
		return nil, e
	}
	if exists != 0 {
		return nil, ErrMaintenanceConflict
	}
	_, e = tx.Exec("INSERT INTO maintenance_tasks(id,public_id,kind,target_version,status,created_by,created_at,updated_at,last_seq) VALUES(?,?,?,?,?,?,?,?,0)", id, publicID, kind, target, "queued", by, now, now)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.GetMaintenance(id)
}
func (s *Store) GetMaintenance(id string) (*MaintenanceTask, error) {
	var t MaintenanceTask
	var x sql.NullString
	e := s.db.QueryRow("SELECT id,public_id,kind,target_version,status,created_by,created_at,updated_at,last_seq FROM maintenance_tasks WHERE id=?", id).Scan(&t.ID, &t.PublicID, &t.Kind, &x, &t.Status, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.LastSeq)
	if x.Valid {
		t.TargetVersion = x.String
	}
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, e
}
func (s *Store) ListMaintenance(publicID string, before int64, limit int) ([]MaintenanceTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	q := "SELECT rowid,id,public_id,kind,target_version,status,created_by,created_at,updated_at,last_seq FROM maintenance_tasks WHERE (?='' OR public_id=?)"
	args := []any{publicID, publicID}
	if before > 0 {
		q += " AND rowid<?"
		args = append(args, before)
	}
	q += " ORDER BY rowid DESC LIMIT ?"
	args = append(args, limit+1)
	rows, e := s.db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []MaintenanceTask{}
	for rows.Next() {
		var t MaintenanceTask
		var x sql.NullString
		if e = rows.Scan(&t.Cursor, &t.ID, &t.PublicID, &t.Kind, &x, &t.Status, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.LastSeq); e != nil {
			return nil, e
		}
		if x.Valid {
			t.TargetVersion = x.String
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) ApplyMaintenanceEvent(publicID string, v MaintenanceEvent) (*MaintenanceTask, error) {
	if !maintenanceStatuses[v.Status] || v.Seq < 1 || v.Seq > 4096 || len(v.Message) > 2048 || !utf8.ValidString(v.Message) || v.CreatedAt <= 0 || v.CreatedAt > time.Now().Add(24*time.Hour).Unix() {
		return nil, fmt.Errorf("invalid maintenance event")
	}
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var t MaintenanceTask
	var x sql.NullString
	e = tx.QueryRow("SELECT id,public_id,kind,target_version,status,created_by,created_at,updated_at,last_seq FROM maintenance_tasks WHERE id=?", v.TaskID).Scan(&t.ID, &t.PublicID, &t.Kind, &x, &t.Status, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.LastSeq)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if x.Valid {
		t.TargetVersion = x.String
	}
	if t.PublicID != publicID {
		return nil, ErrNotFound
	}
	if v.Seq <= t.LastSeq {
		var st, msg string
		var ts int64
		if tx.QueryRow("SELECT status,message,created_at FROM maintenance_events WHERE task_id=? AND seq=?", v.TaskID, v.Seq).Scan(&st, &msg, &ts) == nil && st == v.Status && msg == v.Message && ts == v.CreatedAt {
			return &t, nil
		}
		return nil, ErrMaintenanceConflict
	}
	if v.Seq != t.LastSeq+1 {
		return nil, ErrMaintenanceGap
	}
	if !validMaintenanceTransition(t.Status, v.Status) {
		return nil, ErrMaintenanceConflict
	}
	now := v.CreatedAt
	if now == 0 {
		now = time.Now().Unix()
	}
	if _, e = tx.Exec("INSERT INTO maintenance_events(task_id,seq,status,message,created_at) VALUES(?,?,?,?,?)", v.TaskID, v.Seq, v.Status, v.Message, now); e != nil {
		return nil, e
	}
	if _, e = tx.Exec("UPDATE maintenance_tasks SET status=?,updated_at=?,last_seq=? WHERE id=?", v.Status, now, v.Seq, v.TaskID); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	t.Status = v.Status
	t.UpdatedAt = now
	t.LastSeq = v.Seq
	return &t, nil
}
func (s *Store) MaintenanceEvents(id string, after int64, limit int) ([]MaintenanceEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, e := s.db.Query("SELECT task_id,seq,status,message,created_at FROM maintenance_events WHERE task_id=? AND seq>? ORDER BY seq LIMIT ?", id, after, limit+1)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []MaintenanceEvent{}
	for rows.Next() {
		var v MaintenanceEvent
		if e = rows.Scan(&v.TaskID, &v.Seq, &v.Status, &v.Message, &v.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
