package cache

import (
	"testing"
	"time"

	"website-gobased/internal/protocol"
)

func TestL1StoreUsesRealLRUEviction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	store := NewL1Store(2, 15*time.Second)
	store.Put(cacheProduct(1), now)
	store.Put(cacheProduct(2), now.Add(time.Second))
	if _, ok := store.Get(1, now.Add(2*time.Second)); !ok {
		t.Fatal("product 1 was not found")
	}
	_, evicted := store.Put(cacheProduct(3), now.Add(3*time.Second))
	if evicted == nil || evicted.ID != 2 {
		t.Fatalf("evicted = %#v; want product 2", evicted)
	}
	if _, ok := store.Get(1, now.Add(4*time.Second)); !ok {
		t.Fatal("recent product 1 was evicted")
	}
}

func TestL1StoreExpiresWithoutRefreshingTTLOnHit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	store := NewL1Store(9, 15*time.Second)
	store.Put(cacheProduct(1), now)
	if _, ok := store.Get(1, now.Add(14*time.Second)); !ok {
		t.Fatal("product expired too early")
	}
	if _, ok := store.Get(1, now.Add(15*time.Second)); ok {
		t.Fatal("cache hit refreshed the fixed TTL")
	}
}

func TestL1StoreClearRemovesAllEntries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	store := NewL1Store(9, 15*time.Second)
	store.Put(cacheProduct(1), now)
	store.Put(cacheProduct(2), now)
	removed := store.Clear()
	if len(removed) != 2 || len(store.Snapshot(now)) != 0 {
		t.Fatalf("removed=%#v snapshot=%#v", removed, store.Snapshot(now))
	}
}

func cacheProduct(id uint64) protocol.CacheProduct {
	return protocol.CacheProduct{ID: id, Name: "product", Category: "test", Version: 1}
}
