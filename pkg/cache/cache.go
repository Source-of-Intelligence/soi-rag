package cache

import (
	"container/list"
	"sync"
	"time"
)

// Cache 缓存接口
type Cache interface {
	// Get 获取缓存值
	Get(key string) (interface{}, bool)
	// Set 设置缓存值
	Set(key string, value interface{})
	// SetWithTTL 设置缓存值并指定TTL
	SetWithTTL(key string, value interface{}, ttl time.Duration)
	// Delete 删除缓存值
	Delete(key string)
	// Clear 清空缓存
	Clear()
	// Stats 获取缓存统计信息
	Stats() Stats
}

// Stats 缓存统计信息
type Stats struct {
	Hits        int64   // 命中次数
	Misses      int64   // 未命中次数
	TotalItems  int     // 当前缓存项数量
	MaxCapacity int     // 最大容量
	HitRate     float64 // 命中率
	Evictions   int64   // 驱逐次数
}

// entry 缓存条目
type entry struct {
	key       string
	value     interface{}
	expiresAt time.Time // 过期时间，零值表示永不过期
	element   *list.Element
}

// LRUCache LRU缓存实现
type LRUCache struct {
	mu        sync.RWMutex
	items     map[string]*entry
	evictList *list.List // LRU链表，前端是最近使用的
	maxSize   int
	ttl       time.Duration // 默认TTL
	stats     Stats
}

// Option 缓存配置选项
type Option func(*LRUCache)

// WithMaxSize 设置最大容量
func WithMaxSize(size int) Option {
	return func(c *LRUCache) {
		if size > 0 {
			c.maxSize = size
		}
	}
}

// WithTTL 设置默认TTL
func WithTTL(ttl time.Duration) Option {
	return func(c *LRUCache) {
		c.ttl = ttl
	}
}

// NewLRUCache 创建LRU缓存
func NewLRUCache(opts ...Option) *LRUCache {
	c := &LRUCache{
		items:     make(map[string]*entry),
		evictList: list.New(),
		maxSize:   1000, // 默认容量
		ttl:       0,    // 默认永不过期
	}
	for _, opt := range opts {
		opt(c)
	}
	c.stats.MaxCapacity = c.maxSize
	return c
}

// Get 获取缓存值
func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ent, ok := c.items[key]
	if !ok {
		c.stats.Misses++
		return nil, false
	}

	// 检查是否过期
	if !ent.expiresAt.IsZero() && time.Now().After(ent.expiresAt) {
		// 已过期，删除
		c.deleteEntryLocked(ent)
		c.stats.Misses++
		return nil, false
	}

	// 更新LRU顺序（移到前端）
	c.evictList.MoveToFront(ent.element)
	c.stats.Hits++
	c.updateHitRate()
	return ent.value, true
}

// Set 设置缓存值（使用默认TTL）
func (c *LRUCache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL 设置缓存值并指定TTL
func (c *LRUCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查是否已存在
	if ent, ok := c.items[key]; ok {
		// 更新现有条目
		ent.value = value
		if ttl > 0 {
			ent.expiresAt = time.Now().Add(ttl)
		} else {
			ent.expiresAt = time.Time{}
		}
		c.evictList.MoveToFront(ent.element)
		return
	}

	// 检查容量，必要时驱逐
	c.evictIfNeededLocked()

	// 创建新条目
	ent := &entry{
		key:   key,
		value: value,
	}
	if ttl > 0 {
		ent.expiresAt = time.Now().Add(ttl)
	}
	ent.element = c.evictList.PushFront(key)
	c.items[key] = ent
	c.stats.TotalItems = len(c.items)
}

// Delete 删除缓存值
func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ent, ok := c.items[key]; ok {
		c.deleteEntryLocked(ent)
	}
}

// Clear 清空缓存
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*entry)
	c.evictList = list.New()
	c.stats.TotalItems = 0
	c.stats.Hits = 0
	c.stats.Misses = 0
	c.stats.Evictions = 0
	c.stats.HitRate = 0
}

// Stats 获取缓存统计信息
func (c *LRUCache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.TotalItems = len(c.items)
	return stats
}

// deleteEntryLocked 删除条目（调用前必须持有锁）
func (c *LRUCache) deleteEntryLocked(ent *entry) {
	c.evictList.Remove(ent.element)
	delete(c.items, ent.key)
	c.stats.TotalItems = len(c.items)
}

// evictIfNeededLocked 必要时驱逐条目（调用前必须持有锁）
func (c *LRUCache) evictIfNeededLocked() {
	// 先清理过期条目
	c.cleanExpiredLocked()

	// 如果仍然超出容量，驱逐最久未使用的
	for len(c.items) >= c.maxSize {
		c.evictOldestLocked()
	}
}

// evictOldestLocked 驱逐最久未使用的条目（调用前必须持有锁）
func (c *LRUCache) evictOldestLocked() {
	if c.evictList.Len() == 0 {
		return
	}

	// 获取链表末尾元素（最久未使用）
	oldest := c.evictList.Back()
	if oldest == nil {
		return
	}

	key := oldest.Value.(string)
	if ent, ok := c.items[key]; ok {
		c.deleteEntryLocked(ent)
		c.stats.Evictions++
	}
}

// cleanExpiredLocked 清理过期条目（调用前必须持有锁）
func (c *LRUCache) cleanExpiredLocked() {
	now := time.Now()
	var keysToDelete []string

	for key, ent := range c.items {
		if !ent.expiresAt.IsZero() && now.After(ent.expiresAt) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		if ent, ok := c.items[key]; ok {
			c.deleteEntryLocked(ent)
			c.stats.Evictions++
		}
	}
}

// updateHitRate 更新命中率（调用前必须持有锁）
func (c *LRUCache) updateHitRate() {
	total := c.stats.Hits + c.stats.Misses
	if total > 0 {
		c.stats.HitRate = float64(c.stats.Hits) / float64(total)
	}
}

// PurgeExpired 清理所有过期条目（公开方法）
func (c *LRUCache) PurgeExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	before := len(c.items)
	c.cleanExpiredLocked()
	return before - len(c.items)
}

// Keys 返回所有键（用于调试）
func (c *LRUCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	return keys
}

// Size 返回当前缓存大小
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// GetOrSet 获取缓存值，如果不存在则调用 fn 计算并设置
func (c *LRUCache) GetOrSet(key string, fn func() (interface{}, error), ttl ...time.Duration) (interface{}, error) {
	if val, ok := c.Get(key); ok {
		return val, nil
	}
	val, err := fn()
	if err != nil {
		return nil, err
	}
	if len(ttl) > 0 && ttl[0] > 0 {
		c.SetWithTTL(key, val, ttl[0])
	} else {
		c.Set(key, val)
	}
	return val, nil
}

// NewLRUCacheWithTTL 创建带默认TTL的LRU缓存
func NewLRUCacheWithTTL(maxSize int, defaultTTL time.Duration) *LRUCache {
	return NewLRUCache(WithMaxSize(maxSize), WithTTL(defaultTTL))
}
