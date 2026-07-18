package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"website-gobased/internal/protocol"
)

type redisStore struct {
	client  *redis.Client
	prefix  string
	baseTTL time.Duration
	jitter  int
}

func newRedisStore(address, labID string, baseTTL time.Duration, jitter int) *redisStore {
	return &redisStore{
		client: redis.NewClient(&redis.Options{
			Addr: address, DialTimeout: 800 * time.Millisecond,
			ReadTimeout: 800 * time.Millisecond, WriteTimeout: 800 * time.Millisecond,
		}),
		prefix: "labcache:" + labID + ":", baseTTL: baseTTL, jitter: jitter,
	}
}

func (s *redisStore) Close() error { return s.client.Close() }

func (s *redisStore) productKey(productID uint64) string {
	return s.prefix + "product:" + strconv.FormatUint(productID, 10)
}

func (s *redisStore) summaryKey(instanceID string) string {
	return s.prefix + "l1:" + instanceID
}

func (s *redisStore) Get(ctx context.Context, productID uint64) (protocol.CacheProduct, bool, error) {
	value, err := s.client.Get(ctx, s.productKey(productID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return protocol.CacheProduct{}, false, nil
	}
	if err != nil {
		return protocol.CacheProduct{}, false, err
	}
	var product protocol.CacheProduct
	if err := json.Unmarshal(value, &product); err != nil || product.ID != productID {
		_ = s.client.Del(context.WithoutCancel(ctx), s.productKey(productID)).Err()
		return protocol.CacheProduct{}, false, errors.New("redis product value is invalid")
	}
	return product, true, nil
}

func (s *redisStore) Set(ctx context.Context, product protocol.CacheProduct) (time.Time, error) {
	value, err := json.Marshal(product)
	if err != nil {
		return time.Time{}, err
	}
	ttl := s.baseTTL
	if s.jitter > 0 {
		rangeMS := int64(s.baseTTL/time.Millisecond) * int64(s.jitter) / 100
		delta := rand.Int64N(rangeMS*2+1) - rangeMS
		ttl += time.Duration(delta) * time.Millisecond
	}
	if err := s.client.Set(ctx, s.productKey(product.ID), value, ttl).Err(); err != nil {
		return time.Time{}, err
	}
	return time.Now().UTC().Add(ttl), nil
}

func (s *redisStore) Delete(ctx context.Context, productID uint64) error {
	return s.client.Del(ctx, s.productKey(productID)).Err()
}

func (s *redisStore) Report(ctx context.Context, instance protocol.CacheInstanceState) error {
	value, err := json.Marshal(instance)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.summaryKey(instance.InstanceID), value, 5*time.Second).Err()
}

func (s *redisStore) State(
	ctx context.Context,
	catalog []protocol.CacheProduct,
	instanceIDs []string,
	now time.Time,
) ([]protocol.CacheInstanceState, protocol.CacheRedisState) {
	instances := make([]protocol.CacheInstanceState, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		value, err := s.client.Get(ctx, s.summaryKey(instanceID)).Bytes()
		if err != nil {
			instances = append(instances, protocol.CacheInstanceState{
				InstanceID: instanceID, Status: "stale", ObservedAt: now, Entries: []protocol.CacheEntry{},
			})
			continue
		}
		var state protocol.CacheInstanceState
		if json.Unmarshal(value, &state) != nil || state.InstanceID != instanceID {
			state = protocol.CacheInstanceState{InstanceID: instanceID, Status: "stale", ObservedAt: now, Entries: []protocol.CacheEntry{}}
		}
		instances = append(instances, state)
	}
	redisState := protocol.CacheRedisState{Status: "running", ObservedAt: now, Entries: []protocol.CacheEntry{}}
	pipe := s.client.Pipeline()
	gets := make([]*redis.StringCmd, len(catalog))
	ttls := make([]*redis.DurationCmd, len(catalog))
	for index, product := range catalog {
		gets[index] = pipe.Get(ctx, s.productKey(product.ID))
		ttls[index] = pipe.PTTL(ctx, s.productKey(product.ID))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		redisState.Status = "unavailable"
		return instances, redisState
	}
	for index, command := range gets {
		value, err := command.Bytes()
		if err != nil {
			continue
		}
		var product protocol.CacheProduct
		if json.Unmarshal(value, &product) != nil || product.ID != catalog[index].ID {
			continue
		}
		ttl := ttls[index].Val()
		if ttl <= 0 {
			continue
		}
		redisState.Entries = append(redisState.Entries, protocol.CacheEntry{
			Product: product, ExpiresAt: now.Add(ttl), TTLMS: ttl.Milliseconds(),
		})
	}
	return instances, redisState
}

func (s *redisStore) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping cache redis: %w", err)
	}
	return nil
}
