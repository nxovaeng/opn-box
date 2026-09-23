package ttlcleaner

import (
	"container/heap"
	"net"
	"sync"
	"time"
)

// Entry represents a single IP address managed in a PF table with an expiration timestamp.
type Entry struct {
	Table    string
	IPStr    string
	RawIP    net.IP
	ExpireAt time.Time
	index    int // index in the heap
}

// entryHeap implements heap.Interface for Entry pointers ordered by ExpireAt (earliest first).
type entryHeap []*Entry

func (h entryHeap) Len() int           { return len(h) }
func (h entryHeap) Less(i, j int) bool { return h[i].ExpireAt.Before(h[j].ExpireAt) }
func (h entryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *entryHeap) Push(x any) {
	n := len(*h)
	item := x.(*Entry)
	item.index = n
	*h = append(*h, item)
}

func (h *entryHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// Tracker manages TTL expiration across multiple tables in a thread-safe manner.
type Tracker struct {
	mu     sync.Mutex
	pq     entryHeap
	tables map[string]map[string]*Entry // table -> ipStr -> *Entry

	// Metrics
	totalAdded   uint64
	totalRenewed uint64
	totalExpired uint64
	totalDeleted uint64
}

// NewTracker creates a new TTL Tracker instance.
func NewTracker() *Tracker {
	t := &Tracker{
		pq:     make(entryHeap, 0, 1024),
		tables: make(map[string]map[string]*Entry),
	}
	heap.Init(&t.pq)
	return t
}

// AddOrRenew registers an IP with a given TTL.
// Returns isNew = true if the IP was not previously present (requires insertion into PF table).
// Returns isNew = false if the IP was already tracked and its TTL was renewed/extended.
func (t *Tracker) AddOrRenew(table string, ip net.IP, ttl time.Duration) (isNew bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ipStr := ip.String()
	now := time.Now()
	newExpireAt := now.Add(ttl)

	tableMap, exists := t.tables[table]
	if !exists {
		tableMap = make(map[string]*Entry)
		t.tables[table] = tableMap
	}

	entry, found := tableMap[ipStr]
	if found {
		// Existing entry: renew expiration time if new expireAt is further in the future
		if newExpireAt.After(entry.ExpireAt) {
			entry.ExpireAt = newExpireAt
			heap.Fix(&t.pq, entry.index)
		}
		t.totalRenewed++
		return false
	}

	// New entry
	entry = &Entry{
		Table:    table,
		IPStr:    ipStr,
		RawIP:    ip,
		ExpireAt: newExpireAt,
	}
	tableMap[ipStr] = entry
	heap.Push(&t.pq, entry)
	t.totalAdded++
	return true
}

// PopExpired retrieves all entries that have expired relative to `now`, up to `maxBatch`.
// Returns a map grouping expired IPs by their table name, ready for batch deletion.
func (t *Tracker) PopExpired(now time.Time, maxBatch int) map[string][]net.IP {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[string][]net.IP)
	count := 0

	for t.pq.Len() > 0 && count < maxBatch {
		top := t.pq[0]
		if top.ExpireAt.After(now) {
			// Since it's a min-heap, if top is not expired, none of the rest are.
			break
		}

		// Pop expired entry
		expired := heap.Pop(&t.pq).(*Entry)
		if tableMap, ok := t.tables[expired.Table]; ok {
			delete(tableMap, expired.IPStr)
			if len(tableMap) == 0 {
				delete(t.tables, expired.Table)
			}
		}

		result[expired.Table] = append(result[expired.Table], expired.RawIP)
		count++
		t.totalExpired++
	}

	return result
}

// Delete explicitly removes an IP from tracking (e.g. on manual delete command).
func (t *Tracker) Delete(table string, ip net.IP) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	tableMap, ok := t.tables[table]
	if !ok {
		return false
	}

	ipStr := ip.String()
	entry, found := tableMap[ipStr]
	if !found {
		return false
	}

	heap.Remove(&t.pq, entry.index)
	delete(tableMap, ipStr)
	if len(tableMap) == 0 {
		delete(t.tables, table)
	}

	t.totalDeleted++
	return true
}

// Flush removes all entries for a specific table.
// Returns the list of IPs that were removed.
func (t *Tracker) Flush(table string) []net.IP {
	t.mu.Lock()
	defer t.mu.Unlock()

	tableMap, ok := t.tables[table]
	if !ok {
		return nil
	}

	ips := make([]net.IP, 0, len(tableMap))
	for _, entry := range tableMap {
		heap.Remove(&t.pq, entry.index)
		ips = append(ips, entry.RawIP)
		t.totalDeleted++
	}
	delete(t.tables, table)
	return ips
}

// Stats returns a snapshot of tracker metrics.
type Stats struct {
	ActiveEntries int
	TotalAdded    uint64
	TotalRenewed  uint64
	TotalExpired  uint64
	TotalDeleted  uint64
}

func (t *Tracker) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()

	return Stats{
		ActiveEntries: t.pq.Len(),
		TotalAdded:    t.totalAdded,
		TotalRenewed:  t.totalRenewed,
		TotalExpired:  t.totalExpired,
		TotalDeleted:  t.totalDeleted,
	}
}
