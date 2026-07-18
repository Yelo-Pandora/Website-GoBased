package cache

import (
	"container/list"
	"sync"
	"time"

	"website-gobased/internal/protocol"
)

type l1Value struct {
	product        protocol.CacheProduct
	expiresAt      time.Time
	lastAccessedAt time.Time
}

// L1Store is one process-local bounded LRU with fixed write-time TTL.
type L1Store struct {
	mu      sync.Mutex
	maximum int
	ttl     time.Duration
	entries map[uint64]*list.Element
	recency *list.List
}

func NewL1Store(maximum int, ttl time.Duration) *L1Store {
	return &L1Store{
		maximum: maximum,
		ttl:     ttl,
		entries: make(map[uint64]*list.Element, maximum),
		recency: list.New(),
	}
}

func (s *L1Store) Get(productID uint64, now time.Time) (protocol.CacheProduct, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	element, ok := s.entries[productID]
	if !ok {
		return protocol.CacheProduct{}, false
	}
	value := element.Value.(*l1Value)
	if !now.Before(value.expiresAt) {
		s.remove(element)
		return protocol.CacheProduct{}, false
	}
	value.lastAccessedAt = now
	s.recency.MoveToFront(element)
	return value.product, true
}

func (s *L1Store) Put(product protocol.CacheProduct, now time.Time) (time.Time, *protocol.CacheProduct) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiresAt := now.Add(s.ttl)
	if element, ok := s.entries[product.ID]; ok {
		value := element.Value.(*l1Value)
		value.product = product
		value.expiresAt = expiresAt
		value.lastAccessedAt = now
		s.recency.MoveToFront(element)
		return expiresAt, nil
	}
	element := s.recency.PushFront(&l1Value{
		product: product, expiresAt: expiresAt, lastAccessedAt: now,
	})
	s.entries[product.ID] = element
	if len(s.entries) <= s.maximum {
		return expiresAt, nil
	}
	oldest := s.recency.Back()
	evicted := oldest.Value.(*l1Value).product
	s.remove(oldest)
	return expiresAt, &evicted
}

func (s *L1Store) Delete(productID uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	element, ok := s.entries[productID]
	if !ok {
		return false
	}
	s.remove(element)
	return true
}

func (s *L1Store) Snapshot(now time.Time) []protocol.CacheEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make([]protocol.CacheEntry, 0, len(s.entries))
	for element := s.recency.Front(); element != nil; {
		next := element.Next()
		value := element.Value.(*l1Value)
		if !now.Before(value.expiresAt) {
			s.remove(element)
		} else {
			accessed := value.lastAccessedAt
			values = append(values, protocol.CacheEntry{
				Product: value.product, ExpiresAt: value.expiresAt,
				LastAccessedAt: &accessed,
			})
		}
		element = next
	}
	return values
}

func (s *L1Store) remove(element *list.Element) {
	delete(s.entries, element.Value.(*l1Value).product.ID)
	s.recency.Remove(element)
}
