package chatcore

import (
	"sync"
	"time"
)

// EventDedupeTTL is how long a message ID is remembered for de-duplication.
const EventDedupeTTL = 10 * time.Minute

// RecentKeys remembers keys for a TTL so redelivered chat events are handled once.
type RecentKeys struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]time.Time
}

// NewRecentKeys creates an empty de-duplication set.
func NewRecentKeys(ttl time.Duration) *RecentKeys {
	return &RecentKeys{ttl: ttl, seen: map[string]time.Time{}}
}

// FirstSeen records key and reports whether it was not seen within the TTL.
func (r *RecentKeys) FirstSeen(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for k, at := range r.seen {
		if now.Sub(at) > r.ttl {
			delete(r.seen, k)
		}
	}
	if _, ok := r.seen[key]; ok {
		return false
	}
	r.seen[key] = now
	return true
}

// KeyedMutex serialises work per key (one chat thread) and frees idle entries.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*refMutex
}

type refMutex struct {
	sync.Mutex
	refs int
}

// Lock blocks until key is free and returns its unlock function.
func (k *KeyedMutex) Lock(key string) (unlock func()) {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*refMutex{}
	}
	m, ok := k.locks[key]
	if !ok {
		m = &refMutex{}
		k.locks[key] = m
	}
	m.refs++
	k.mu.Unlock()

	m.Lock()
	return func() {
		m.Unlock()
		k.mu.Lock()
		if m.refs--; m.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
