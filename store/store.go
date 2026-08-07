package store

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database connection.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: serialize writes, avoid "database is locked"

	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hosts (
			public_id TEXT PRIMARY KEY,
			token TEXT UNIQUE NOT NULL,
			nickname TEXT NOT NULL,
			tags TEXT,
			host_id TEXT,
			hostname TEXT,
			os TEXT,
			platform TEXT,
			platform_version TEXT,
			kernel_arch TEXT,
			model_name TEXT,
			cores INTEGER,
			virtualization_role TEXT,
			boot_time INTEGER,
			last_info_json TEXT,
			last_seen INTEGER,
			created_at INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS usages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			public_id TEXT NOT NULL,
			ts INTEGER NOT NULL,
			cpu_usage REAL,
			memory_total INTEGER,
			memory_used INTEGER,
			swap_total INTEGER,
			swap_used INTEGER,
			disk_total INTEGER,
			disk_used INTEGER,
			net_recv INTEGER,
			net_send INTEGER,
			net_recv_speed REAL,
			net_send_speed REAL,
			load1 REAL,
			load5 REAL,
			load15 REAL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usages_pub_ts ON usages(public_id, ts)`,
		`CREATE INDEX IF NOT EXISTS idx_usages_ts ON usages(ts)`,
		`CREATE TABLE IF NOT EXISTS usages_5m (
			public_id TEXT NOT NULL,
			bucket_ts INTEGER NOT NULL,
			cpu_usage REAL,
			memory_total INTEGER,
			memory_used INTEGER,
			swap_total INTEGER,
			swap_used INTEGER,
			disk_total INTEGER,
			disk_used INTEGER,
			net_recv INTEGER,
			net_send INTEGER,
			net_recv_speed REAL,
			net_send_speed REAL,
			load1 REAL,
			load5 REAL,
			load15 REAL,
			PRIMARY KEY (public_id, bucket_ts)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usages_5m_ts ON usages_5m(bucket_ts)`,
		`CREATE TABLE IF NOT EXISTS usages_1h (
			public_id TEXT NOT NULL,
			bucket_ts INTEGER NOT NULL,
			cpu_usage REAL,
			memory_total INTEGER,
			memory_used INTEGER,
			swap_total INTEGER,
			swap_used INTEGER,
			disk_total INTEGER,
			disk_used INTEGER,
			net_recv INTEGER,
			net_send INTEGER,
			net_recv_speed REAL,
			net_send_speed REAL,
			load1 REAL,
			load5 REAL,
			load15 REAL,
			PRIMARY KEY (public_id, bucket_ts)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usages_1h_ts ON usages_1h(bucket_ts)`,
		`CREATE TABLE IF NOT EXISTS tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS host_tags (
			public_id TEXT NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY(public_id, tag_id)
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS notification_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tag TEXT NOT NULL,
			url TEXT NOT NULL,
			notify_online INTEGER NOT NULL DEFAULT 1,
			notify_offline INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	// Add columns that may be missing on databases created by older versions.
	s.ensureColumn("hosts", "tags", "TEXT")
	if err := s.migrateHostTags(); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds a column to a table if it does not already exist.
// Errors (e.g. "duplicate column name") are ignored.
func (s *Store) ensureColumn(table, column, typ string) {
	_, _ = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + typ)
}

// PruneUsages deletes raw usage rows older than retentionDays.
func (s *Store) PruneUsages(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	return s.pruneByTs("usages", "ts", cutoff)
}

// VacuumIfFragmented runs a full VACUUM (followed by a truncating checkpoint,
// which is what actually shrinks the file in WAL mode) when more than half of
// the database pages are free. Returns true when a VACUUM ran.
func (s *Store) VacuumIfFragmented() (bool, error) {
	var freelist, pages int
	if err := s.db.QueryRow(`PRAGMA freelist_count`).Scan(&freelist); err != nil {
		return false, err
	}
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		return false, err
	}
	if freelist*2 <= pages {
		return false, nil
	}
	if _, err := s.db.Exec(`VACUUM`); err != nil {
		return false, err
	}
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return false, err
	}
	return true, nil
}
