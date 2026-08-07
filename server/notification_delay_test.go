package server

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestOfflineNotificationFiresAfterDelay(t *testing.T) {
	s := &Server{
		hub:                       NewHub(),
		offlineNotificationTimers: make(map[string]*time.Timer),
	}
	notified := make(chan struct{}, 1)

	s.scheduleOfflineNotificationAfter("host-1", 20*time.Millisecond, func() {
		notified <- struct{}{}
	})

	select {
	case <-notified:
	case <-time.After(time.Second):
		t.Fatal("offline notification did not fire after delay")
	}
}

func TestOfflineNotificationIsCancelledOnReconnect(t *testing.T) {
	s := &Server{
		hub:                       NewHub(),
		offlineNotificationTimers: make(map[string]*time.Timer),
	}
	var notificationCount atomic.Int32

	s.scheduleOfflineNotificationAfter("host-1", 20*time.Millisecond, func() {
		notificationCount.Add(1)
	})
	if !s.cancelOfflineNotification("host-1") {
		t.Fatal("cancelOfflineNotification() = false, want true (pending timer canceled)")
	}
	if s.cancelOfflineNotification("host-1") {
		t.Fatal("second cancelOfflineNotification() = true, want false (no pending timer)")
	}
	time.Sleep(60 * time.Millisecond)

	if got := notificationCount.Load(); got != 0 {
		t.Fatalf("notification count = %d, want 0", got)
	}
}
