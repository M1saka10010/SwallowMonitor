package store

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database connection.
type Store struct {
	db                *sql.DB
	legacyTokenColumn bool
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
			public_key TEXT UNIQUE,
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
		`CREATE TABLE IF NOT EXISTS host_enrollments (
			public_id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL UNIQUE,
			expires_at INTEGER NOT NULL,
			consumed INTEGER NOT NULL DEFAULT 0
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
	if err := s.migratePublicKeys(); err != nil {
		return err
	}
	if err := s.migrateHostTags(); err != nil {
		return err
	}
	return nil
}

func (s *Store) migratePublicKeys() error {
	rows, err := s.db.Query("PRAGMA table_info(hosts)")
	if err != nil {
		return err
	}
	var hasPublicKey bool
	var publicKeyNotNull bool
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		hasPublicKey = hasPublicKey || name == "public_key"
		if name == "public_key" {
			publicKeyNotNull = notNull != 0
		}
		s.legacyTokenColumn = s.legacyTokenColumn || name == "token"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasPublicKey {
		// Legacy hosts remain unbound; a token is never promoted to a key.
		if _, err := s.db.Exec("ALTER TABLE hosts ADD COLUMN public_key TEXT"); err != nil {
			return err
		}
	}
	if publicKeyNotNull {
		if err := s.rebuildHostsNullable(); err != nil {
			return err
		}
		s.legacyTokenColumn = false
	}
	_, err = s.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_hosts_public_key ON hosts(public_key)")
	return err
}

func (s *Store) rebuildHostsNullable() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE hosts_new (public_id TEXT PRIMARY KEY, public_key TEXT UNIQUE, nickname TEXT NOT NULL, tags TEXT, host_id TEXT, hostname TEXT, os TEXT, platform TEXT, platform_version TEXT, kernel_arch TEXT, model_name TEXT, cores INTEGER, virtualization_role TEXT, boot_time INTEGER, last_info_json TEXT, last_seen INTEGER, created_at INTEGER)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO hosts_new SELECT public_id, public_key, nickname, tags, host_id, hostname, os, platform, platform_version, kernel_arch, model_name, cores, virtualization_role, boot_time, last_info_json, last_seen, created_at FROM hosts`); err != nil {
		return err
	}
	if _, err = tx.Exec(`DROP TABLE hosts`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE hosts_new RENAME TO hosts`); err != nil {
		return err
	}
	return tx.Commit()
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
