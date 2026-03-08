package cache

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	staffKeyPrefix      = "staff:"
	redemptionKeyPrefix = "redemption:"

	// Staff mappings are essentially static (loaded from CSV once),
	// so a long TTL is appropriate.
	staffTTL = 1 * time.Hour

	// Redemption TTL is long (24 hours) because:
	// - Redemptions only go false→true and never reverse via normal flow
	// - Short TTL creates re-redemption risk windows
	// - TTL is only a safety net for direct SQL edits that bypass the API
	redemptionTTL = 24 * time.Hour
)

// RedisCache implements CacheStore using Redis.
type RedisCache struct {
	client *goredis.Client
}

// NewRedisCache creates a new RedisCache wrapping the given Redis client.
func NewRedisCache(client *goredis.Client) *RedisCache {
	return &RedisCache{client: client}
}

func (r *RedisCache) GetStaffTeam(ctx context.Context, staffPassID string) (string, bool, error) {
	key := staffKeyPrefix + staffPassID
	val, err := r.client.Get(ctx, key).Result()
	if err == goredis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("redis GET %s: %w", key, err)
	}
	return val, true, nil
}

func (r *RedisCache) SetStaffTeam(ctx context.Context, staffPassID string, teamName string) error {
	key := staffKeyPrefix + staffPassID
	return r.client.Set(ctx, key, teamName, staffTTL).Err()
}

func (r *RedisCache) GetRedemptionStatus(ctx context.Context, teamName string) (bool, bool, error) {
	key := redemptionKeyPrefix + teamName
	val, err := r.client.Get(ctx, key).Result()
	if err == goredis.Nil {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("redis GET %s: %w", key, err)
	}
	return val == "true", true, nil
}

func (r *RedisCache) SetRedemptionStatus(ctx context.Context, teamName string) error {
	key := redemptionKeyPrefix + teamName
	return r.client.Set(ctx, key, "true", redemptionTTL).Err()
}

func (r *RedisCache) InvalidateRedemption(ctx context.Context, teamName string) error {
	key := redemptionKeyPrefix + teamName
	return r.client.Del(ctx, key).Err()
}

func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisCache) Close() error {
	return r.client.Close()
}
