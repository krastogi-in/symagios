package utils

import (
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	defaultCacheSize = 100
)

// LRUCache is a thread-safe LRU cache keyed by string.
type LRUCache[V any] struct {
	mu    sync.RWMutex
	cache *lru.Cache[string, V]
}

// NewLRUCache creates an LRU cache with the given capacity.
func NewLRUCache[V any](cacheSize int) *LRUCache[V] {
	if cacheSize <= 0 {
		cacheSize = defaultCacheSize
	}
	c, _ := lru.New[string, V](cacheSize)
	return &LRUCache[V]{cache: c}
}

func (c *LRUCache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Get(key)
}

func (c *LRUCache[V]) Add(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Add(key, value)
}

func (c *LRUCache[V]) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Remove(key)
}

func (c *LRUCache[V]) Close() {}

// Cache stores string summaries with a latest-key helper for checkpoint flows.
type Cache struct {
	lock      sync.Mutex
	cache     *LRUCache[string]
	latestKey string
}

func NewCache(cacheSize int) *Cache {
	return &Cache{
		cache: NewLRUCache[string](cacheSize),
	}
}

func (s *Cache) Add(key string, value string) {
	s.cache.Add(key, value)
	s.lock.Lock()
	s.latestKey = key
	s.lock.Unlock()
}

func (s *Cache) Get(key string) (string, bool) {
	return s.cache.Get(key)
}

func (s *Cache) GetLatestSummary() string {
	s.lock.Lock()
	defer s.lock.Unlock()
	value, ok := s.cache.Get(s.latestKey)
	if !ok {
		return ""
	}
	return value
}

func (s *Cache) Close() {}
