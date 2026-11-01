package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func maintenanceStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "maintenance.db")
	s := openMaintenanceStore(t, path)
	return s, path
}

func openMaintenanceStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func createMaintenanceTask(t *testing.T, s *Store, host string, now int64) *MaintenanceTask {
	t.Helper()
	task, err := s.CreateMaintenance(host, "update", "v1.2.3", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func applyMaintenanceEvent(t *testing.T, s *Store, host string, event MaintenanceEvent) *MaintenanceTask {
	t.Helper()
	task, err := s.ApplyMaintenanceEvent(host, event)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestMaintenancePersistenceAndEventIdentity(t *testing.T) {
	s, path := maintenanceStore(t)
	now := time.Now().Unix() - 10
	task := createMaintenanceTask(t, s, "host-a", now)
	event := MaintenanceEvent{TaskID: task.ID, Seq: 1, Status: "accepted", Message: "accepted durably", CreatedAt: now + 1}
	want := applyMaintenanceEvent(t, s, task.PublicID, event)
	if want.ID != task.ID || want.Kind != "update" || want.TargetVersion != "v1.2.3" || want.CreatedBy != "admin" || want.CreatedAt != now || want.UpdatedAt != event.CreatedAt || want.LastSeq != 1 || want.Status != "accepted" {
		t.Fatalf("unexpected task: %+v", want)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openMaintenanceStore(t, path)
	got, err := s.GetMaintenance(task.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened task = %+v, %v; want %+v", got, err, want)
	}
	got = applyMaintenanceEvent(t, s, task.PublicID, event)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate changed task: %+v", got)
	}
	for _, tc := range []struct {
		name  string
		event MaintenanceEvent
		host  string
		want  error
	}{
		{"timestamp conflict", MaintenanceEvent{task.ID, 1, "accepted", event.Message, now + 2}, task.PublicID, ErrMaintenanceConflict},
		{"message conflict", MaintenanceEvent{task.ID, 1, "accepted", "different", now + 1}, task.PublicID, ErrMaintenanceConflict},
		{"status conflict", MaintenanceEvent{task.ID, 1, "running", event.Message, now + 1}, task.PublicID, ErrMaintenanceConflict},
		{"gap", MaintenanceEvent{task.ID, 3, "running", "gap", now + 2}, task.PublicID, ErrMaintenanceGap},
		{"foreign duplicate", event, "host-b", ErrNotFound},
		{"foreign next event", MaintenanceEvent{task.ID, 2, "running", "foreign", now + 2}, "host-b", ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.ApplyMaintenanceEvent(tc.host, tc.event); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
	got, err = s.GetMaintenance(task.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected events changed task: %+v, %v", got, err)
	}
	events, err := s.MaintenanceEvents(task.ID, 0, 200)
	if err != nil || !reflect.DeepEqual(events, []MaintenanceEvent{event}) {
		t.Fatalf("events = %+v, %v", events, err)
	}
}

func TestMaintenanceConcurrentCreation(t *testing.T) {
	s, _ := maintenanceStore(t)
	const workers = 24
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.CreateMaintenance("same-host", "update", "v1.2.3", "admin", 0)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, ErrMaintenanceConflict) {
			t.Errorf("unexpected creation error: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("created %d tasks, want 1", created)
	}
	tasks, err := s.ListMaintenance("same-host", 0, 200)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}
}

func TestMaintenanceUnknownBlocksAndVerifyingLogs(t *testing.T) {
	s, _ := maintenanceStore(t)
	task := createMaintenanceTask(t, s, "host", 0)
	now := time.Now().Unix()
	statuses := []string{"accepted", "running", "unknown", "verifying", "verifying", "succeeded"}
	var want []MaintenanceEvent
	for i, status := range statuses {
		event := MaintenanceEvent{task.ID, int64(i + 1), status, fmt.Sprintf("log %d", i), now}
		applyMaintenanceEvent(t, s, task.PublicID, event)
		want = append(want, event)
		if status == "unknown" {
			if _, err := s.CreateMaintenance(task.PublicID, "update", "v2.0.0", "admin", 0); !errors.Is(err, ErrMaintenanceConflict) {
				t.Fatalf("unknown did not block: %v", err)
			}
		}
	}
	events, err := s.MaintenanceEvents(task.ID, 0, 200)
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, %v; want %+v", events, err, want)
	}
	events, err = s.MaintenanceEvents(task.ID, 3, 1)
	if err != nil || !reflect.DeepEqual(events, want[3:5]) {
		t.Fatalf("verifying log page = %+v, %v", events, err)
	}
	createMaintenanceTask(t, s, task.PublicID, 0)
}

func TestMaintenanceTerminalCannotRegress(t *testing.T) {
	for _, terminal := range []string{"succeeded", "failed"} {
		t.Run(terminal, func(t *testing.T) {
			s, _ := maintenanceStore(t)
			task := createMaintenanceTask(t, s, "host", 0)
			now := time.Now().Unix()
			applyMaintenanceEvent(t, s, task.PublicID, MaintenanceEvent{task.ID, 1, "accepted", "", now})
			final := MaintenanceEvent{task.ID, 2, terminal, "finished", now}
			want := applyMaintenanceEvent(t, s, task.PublicID, final)
			applyMaintenanceEvent(t, s, task.PublicID, final)
			for _, status := range []string{"accepted", "running", "verifying", "unknown", "succeeded", "failed"} {
				if _, err := s.ApplyMaintenanceEvent(task.PublicID, MaintenanceEvent{task.ID, 3, status, "late", now}); !errors.Is(err, ErrMaintenanceConflict) {
					t.Errorf("%s -> %s: %v", terminal, status, err)
				}
			}
			got, err := s.GetMaintenance(task.ID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("terminal changed: %+v, %v", got, err)
			}
			events, err := s.MaintenanceEvents(task.ID, 0, 200)
			if err != nil || len(events) != 2 {
				t.Fatalf("terminal events = %+v, %v", events, err)
			}
		})
	}
}

func TestMaintenancePendingRetryPersistenceAndExpiry(t *testing.T) {
	s, path := maintenanceStore(t)
	task := createMaintenanceTask(t, s, "delivered-host", 0)
	for i := 0; i < 2; i++ {
		pending, err := s.PendingMaintenance(task.PublicID)
		if err != nil || pending == nil || pending.ID != task.ID || pending.Status != "delivered" || pending.LastSeq != 0 {
			t.Fatalf("pending = %+v, %v", pending, err)
		}
	}
	// Age the delivered fixture without waiting a minute in the test suite.
	if _, err := s.db.Exec("UPDATE maintenance_tasks SET created_at=? WHERE id=?", time.Now().Unix()-120, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openMaintenanceStore(t, path)
	var delivered int
	if err := s.db.QueryRow("SELECT delivered FROM maintenance_tasks WHERE id=?", task.ID).Scan(&delivered); err != nil || delivered != 1 {
		t.Fatalf("delivered = %d, %v", delivered, err)
	}
	pending, err := s.PendingMaintenance(task.PublicID)
	if err != nil || pending == nil || pending.ID != task.ID || pending.Status != "delivered" {
		t.Fatalf("reopened pending = %+v, %v", pending, err)
	}
	applyMaintenanceEvent(t, s, task.PublicID, MaintenanceEvent{task.ID, 1, "accepted", "", time.Now().Unix()})
	if pending, err := s.PendingMaintenance(task.PublicID); err != nil || pending != nil {
		t.Fatalf("accepted pending = %+v, %v", pending, err)
	}
	old := createMaintenanceTask(t, s, "expired-host", time.Now().Unix()-61)
	fresh := createMaintenanceTask(t, s, "fresh-host", time.Now().Unix())
	if pending, err := s.PendingMaintenance(old.PublicID); err != nil || pending != nil {
		t.Fatalf("expired pending = %+v, %v", pending, err)
	}
	got, err := s.GetMaintenance(old.ID)
	if err != nil || got.Status != "expired" || got.LastSeq != 0 {
		t.Fatalf("expired task = %+v, %v", got, err)
	}
	got, err = s.GetMaintenance(fresh.ID)
	if err != nil || got.Status != "queued" {
		t.Fatalf("fresh task = %+v, %v", got, err)
	}
	createMaintenanceTask(t, s, old.PublicID, 0)
	if pending, err := s.PendingMaintenance("absent-host"); err != nil || pending != nil {
		t.Fatalf("absent pending = %+v, %v", pending, err)
	}
}

func TestMaintenanceRowIDPaginationSameSecond(t *testing.T) {
	s, _ := maintenanceStore(t)
	now := time.Now().Unix()
	var want []string
	for i := 0; i < 7; i++ {
		task := createMaintenanceTask(t, s, "host", now)
		applyMaintenanceEvent(t, s, task.PublicID, MaintenanceEvent{task.ID, 1, "accepted", "", now})
		applyMaintenanceEvent(t, s, task.PublicID, MaintenanceEvent{task.ID, 2, "succeeded", "", now})
		want = append([]string{task.ID}, want...)
		createMaintenanceTask(t, s, fmt.Sprintf("other-%d", i), now)
	}
	var got []string
	var before int64
	for pages := 0; pages < 10; pages++ {
		page, err := s.ListMaintenance("host", before, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > 3 {
			t.Fatalf("page exceeds limit plus lookahead: %d", len(page))
		}
		more := len(page) > 2
		if more {
			page = page[:2]
		}
		for _, task := range page {
			if task.PublicID != "host" || task.CreatedAt != now || task.Cursor <= 0 || (before > 0 && task.Cursor >= before) {
				t.Fatalf("invalid page task: %+v, before=%d", task, before)
			}
			got = append(got, task.ID)
			before = task.Cursor
		}
		if !more {
			break
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paginated IDs = %v, want %v", got, want)
	}
}
