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
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	// Add columns that may be missing on databases created by older versions.
	s.ensureColumn("hosts", "tags", "TEXT")
	return nil
}

// ensureColumn adds a column to a table if it does not already exist.
// Errors (e.g. "duplicate column name") are ignored.
func (s *Store) ensureColumn(table, column, typ string) {
	_, _ = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + typ)
}

// PruneUsages deletes usage rows older than retentionDays.
func (s *Store) PruneUsages(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	res, err := s.db.Exec(`DELETE FROM usages WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
