package ttlcleaner

import (
	"net"
	"testing"
	"time"
)

func TestTrackerAddAndRenew(t *testing.T) {
	tracker := NewTracker()
	ip1 := net.ParseIP("192.0.2.1")
	table := "test_table"

	// 1. Initial add
	isNew := tracker.AddOrRenew(table, ip1, 100*time.Millisecond)
	if !isNew {
		t.Error("Expected isNew=true on initial add")
	}

	stats := tracker.Stats()
	if stats.ActiveEntries != 1 {
		t.Errorf("Expected 1 active entry, got %d", stats.ActiveEntries)
	}

	// 2. Renew with longer TTL
	isNew = tracker.AddOrRenew(table, ip1, 500*time.Millisecond)
	if isNew {
		t.Error("Expected isNew=false on renewal")
	}
	stats = tracker.Stats()
	if stats.ActiveEntries != 1 {
		t.Errorf("Expected still 1 active entry, got %d", stats.ActiveEntries)
	}
	if stats.TotalRenewed != 1 {
		t.Errorf("Expected TotalRenewed=1, got %d", stats.TotalRenewed)
	}
}

func TestTrackerExpirationOrder(t *testing.T) {
	tracker := NewTracker()
	table := "test_table"

	ipShort := net.ParseIP("10.0.0.1")
	ipLong := net.ParseIP("10.0.0.2")

	// ipShort expires in 50ms, ipLong in 300ms
	tracker.AddOrRenew(table, ipShort, 50*time.Millisecond)
	tracker.AddOrRenew(table, ipLong, 300*time.Millisecond)

	// Check immediately -> nothing expired
	expired := tracker.PopExpired(time.Now(), 100)
	if len(expired) != 0 {
		t.Errorf("Expected 0 expired immediately, got %v", expired)
	}

	// Sleep 70ms -> only ipShort should expire
	time.Sleep(70 * time.Millisecond)
	now := time.Now()
	expired = tracker.PopExpired(now, 100)

	if len(expired[table]) != 1 {
		t.Fatalf("Expected 1 expired entry, got %d", len(expired[table]))
	}
	if !expired[table][0].Equal(ipShort) {
		t.Errorf("Expected ipShort to expire first, got %v", expired[table][0])
	}

	// Wait for ipLong to expire
	time.Sleep(250 * time.Millisecond)
	expired = tracker.PopExpired(time.Now(), 100)
	if len(expired[table]) != 1 {
		t.Fatalf("Expected 1 expired entry, got %d", len(expired[table]))
	}
	if !expired[table][0].Equal(ipLong) {
		t.Errorf("Expected ipLong to expire, got %v", expired[table][0])
	}

	if tracker.Stats().ActiveEntries != 0 {
		t.Errorf("Expected 0 remaining entries, got %d", tracker.Stats().ActiveEntries)
	}
}

func TestTrackerExplicitDelete(t *testing.T) {
	tracker := NewTracker()
	table := "test_table"
	ip := net.ParseIP("172.16.0.1")

	tracker.AddOrRenew(table, ip, 10*time.Second)
	deleted := tracker.Delete(table, ip)
	if !deleted {
		t.Error("Expected Delete to return true")
	}

	stats := tracker.Stats()
	if stats.ActiveEntries != 0 {
		t.Errorf("Expected 0 active entries, got %d", stats.ActiveEntries)
	}

	// Deleting again should return false
	deleted = tracker.Delete(table, ip)
	if deleted {
		t.Error("Expected Delete to return false for already deleted entry")
	}
}
