package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func enrollmentStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "enrollment.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.CreateTag("production"); err != nil {
		t.Fatal(err)
	}
	return s
}

func pendingEnrollment(t *testing.T, s *Store) *Enrollment {
	t.Helper()
	e, err := s.CreateEnrollmentHost("pending", []string{"production"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func enrollmentKey(label string) string {
	sum := sha256.Sum256([]byte(label))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func TestEnrollmentPendingStorage(t *testing.T) {
	s := enrollmentStore(t)
	before := time.Now().Unix()
	a, b := pendingEnrollment(t, s), pendingEnrollment(t, s)
	if a.Host.PublicID == b.Host.PublicID || a.Token == b.Token {
		t.Fatal("pending hosts share credentials")
	}
	for _, e := range []*Enrollment{a, b} {
		raw, err := hex.DecodeString(e.Token)
		if err != nil || len(raw) != 32 {
			t.Fatal("token is not 256-bit hex")
		}
		if e.ExpiresAt < before+900 || e.ExpiresAt > time.Now().Unix()+900 {
			t.Fatalf("expiry: %d", e.ExpiresAt)
		}
		var key sql.NullString
		if err := s.db.QueryRow("SELECT public_key FROM hosts WHERE public_id=?", e.Host.PublicID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if key.Valid {
			t.Fatal("pending key must be SQL NULL")
		}
		var hash []byte
		if err := s.db.QueryRow("SELECT token_hash FROM host_enrollments WHERE public_id=?", e.Host.PublicID).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		want := sha256.Sum256([]byte(e.Token))
		if !bytes.Equal(hash, want[:]) {
			t.Fatal("stored credential is not SHA256 token digest")
		}
		h, err := s.GetHost(e.Host.PublicID)
		if err != nil || h.PublicKey != "" || len(h.Tags) != 1 || h.Tags[0] != "production" {
			t.Fatalf("pending host: %+v %v", h, err)
		}
	}
}

func TestEnrollmentCreationRollsBack(t *testing.T) {
	s := enrollmentStore(t)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_enrollment BEFORE INSERT ON host_enrollments BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEnrollmentHost("failed", []string{"production"}); err == nil {
		t.Fatal("expected insert failure")
	}
	for _, table := range []string{"hosts", "host_tags", "host_enrollments"} {
		var count int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("partial creation remained in %s: %d", table, count)
		}
	}
}

func TestEnrollmentExpiryRefreshAndBinding(t *testing.T) {
	s := enrollmentStore(t)
	e := pendingEnrollment(t, s)
	key := enrollmentKey("first")
	for _, now := range []int64{e.ExpiresAt, e.ExpiresAt + 1} {
		if _, err := s.ConsumeEnrollment(e.Token, key, now); err == nil {
			t.Fatalf("accepted expired token at %d", now)
		}
	}
	fresh, err := s.RefreshEnrollment(e.Host.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Token == e.Token {
		t.Fatal("refresh reused token")
	}
	if _, err = s.ConsumeEnrollment(e.Token, key, time.Now().Unix()); err == nil {
		t.Fatal("old token survived refresh")
	}
	id, err := s.ConsumeEnrollment(fresh.Token, key, fresh.ExpiresAt-1)
	if err != nil || id != e.Host.PublicID {
		t.Fatalf("consume: %q %v", id, err)
	}
	if _, err = s.ConsumeEnrollment(fresh.Token, enrollmentKey("replacement"), time.Now().Unix()); err == nil {
		t.Fatal("replay replaced key")
	}
	if _, err = s.RefreshEnrollment(id); err == nil {
		t.Fatal("bound host refreshed")
	}
	h, err := s.GetHost(id)
	if err != nil || h.PublicKey != key {
		t.Fatalf("bound key changed: %+v %v", h, err)
	}
}

func TestEnrollmentDuplicateKeyRollsBack(t *testing.T) {
	s := enrollmentStore(t)
	a, b := pendingEnrollment(t, s), pendingEnrollment(t, s)
	key := enrollmentKey("shared")
	if _, err := s.ConsumeEnrollment(a.Token, key, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeEnrollment(b.Token, key, time.Now().Unix()); err == nil {
		t.Fatal("duplicate key accepted")
	}
	h, err := s.GetHost(b.Host.PublicID)
	if err != nil || h.PublicKey != "" {
		t.Fatalf("failed transaction bound host: %+v %v", h, err)
	}
	if _, err := s.ConsumeEnrollment(b.Token, enrollmentKey("unique"), time.Now().Unix()); err != nil {
		t.Fatalf("failed transaction consumed token: %v", err)
	}
}

func TestEnrollmentConcurrentConsumption(t *testing.T) {
	s := enrollmentStore(t)
	e := pendingEnrollment(t, s)
	start := make(chan struct{})
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := s.ConsumeEnrollment(e.Token, enrollmentKey(fmt.Sprint(i)), time.Now().Unix())
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful consumers: %d", successes)
	}
}

func TestEnrollmentDeleteRemovesCredential(t *testing.T) {
	s := enrollmentStore(t)
	e := pendingEnrollment(t, s)
	if err := s.DeleteHost(e.Host.PublicID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM host_enrollments WHERE public_id=?", e.Host.PublicID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("deleted host retains enrollment")
	}
	if _, err := s.ConsumeEnrollment(e.Token, enrollmentKey("deleted"), time.Now().Unix()); err == nil {
		t.Fatal("deleted host enrolled")
	}
}

func TestEnrollmentLegacyMigration(t *testing.T) {
	for _, legacyToken := range []bool{false, true} {
		t.Run(fmt.Sprint(legacyToken), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			credential := "public_key TEXT NOT NULL UNIQUE"
			value := enrollmentKey("existing")
			if legacyToken {
				credential = "token TEXT NOT NULL UNIQUE"
				value = "legacy-secret"
			}
			_, err = s.db.Exec("DROP TABLE hosts; CREATE TABLE hosts (" +
				"public_id TEXT PRIMARY KEY, " + credential + ", nickname TEXT NOT NULL, tags TEXT," +
				"host_id TEXT, hostname TEXT, os TEXT, platform TEXT, platform_version TEXT," +
				"kernel_arch TEXT, model_name TEXT, cores INTEGER, virtualization_role TEXT," +
				"boot_time INTEGER, last_info_json TEXT, last_seen INTEGER, created_at INTEGER)")
			if err == nil {
				_, err = s.db.Exec("INSERT INTO hosts VALUES ('historical',?,'old',?,'machine','hostname','linux','platform','1','amd64','cpu',4,'guest',123,'{}',456,100)", value, encodeTags([]string{"production"}))
			}
			if err == nil {
				_, err = s.db.Exec("INSERT INTO usages(public_id,ts,cpu_usage) VALUES ('historical',456,12); INSERT INTO usages_5m(public_id,bucket_ts,cpu_usage) VALUES ('historical',300,13); INSERT INTO usages_1h(public_id,bucket_ts,cpu_usage) VALUES ('historical',0,14)")
			}
			if err != nil {
				s.Close()
				t.Fatal(err)
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			pendingEnrollment(t, s)
			pendingEnrollment(t, s)
			h, err := s.GetHost("historical")
			if err != nil || h.Nickname != "old" || h.LastSeen != 456 || len(h.Tags) != 1 || h.Tags[0] != "production" {
				t.Fatalf("lost host metadata: %+v %v", h, err)
			}
			if legacyToken {
				e, err := s.RefreshEnrollment(h.PublicID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.ConsumeEnrollment(e.Token, enrollmentKey("migrated"), time.Now().Unix()); err != nil {
					t.Fatal(err)
				}
			} else if h.PublicKey != value {
				t.Fatal("migration lost bound key")
			}
			for _, table := range []string{"usages", "usages_5m", "usages_1h"} {
				var n int
				if err := s.db.QueryRow("SELECT count(*) FROM " + table + " WHERE public_id='historical'").Scan(&n); err != nil || n != 1 {
					t.Fatalf("lost %s history: %d %v", table, n, err)
				}
			}
		})
	}
}
