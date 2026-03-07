package cache

import (
	"context"
	"sync"
)

// MemoryCache implements CacheStore using an in-memory map.
// It is safe for concurrent use and intended for unit tests
// where a real Redis connection is not available.
type MemoryCache struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemoryCache creates a new in-memory cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{data: make(map[string]string)}
}

func (m *MemoryCache) GetStaffTeam(_ context.Context, staffPassID string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, found := m.data[staffKeyPrefix+staffPassID]
	return val, found, nil
}

func (m *MemoryCache) SetStaffTeam(_ context.Context, staffPassID string, teamName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[staffKeyPrefix+staffPassID] = teamName
	return nil
}

func (m *MemoryCache) GetRedemptionStatus(_ context.Context, teamName string) (bool, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, found := m.data[redemptionKeyPrefix+teamName]
	if !found {
		return false, false, nil
	}
	return val == "true", true, nil
}

func (m *MemoryCache) SetRedemptionNX(_ context.Context, teamName string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := redemptionKeyPrefix + teamName
	if _, exists := m.data[key]; exists {
		return false, nil // key already exists, NX fails
	}
	m.data[key] = "true"
	return true, nil // key set successfully
}

func (m *MemoryCache) InvalidateRedemption(_ context.Context, teamName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, redemptionKeyPrefix+teamName)
	return nil
}

func (m *MemoryCache) Ping(_ context.Context) error {
	return nil
}

func (m *MemoryCache) Close() error {
	return nil
}
