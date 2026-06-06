package cache

import (
	"testing"
	"time"
)

func TestLRUCache_SetAndGet(t *testing.T) {
	c := NewLRUCache(WithMaxSize(10))
	c.Set("key1", "value1")

	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("应能获取已设置的 key")
	}
	if val != "value1" {
		t.Errorf("值应为 value1，实际为 %v", val)
	}
}

func TestLRUCache_GetNonExistent(t *testing.T) {
	c := NewLRUCache(WithMaxSize(10))
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("不存在的 key 应返回 false")
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	c := NewLRUCache(WithMaxSize(3))
	c.Set("key1", "val1")
	c.Set("key2", "val2")
	c.Set("key3", "val3")
	c.Set("key4", "val4") // 应驱逐 key1

	_, ok1 := c.Get("key1")
	if ok1 {
		t.Error("key1 应被驱逐")
	}

	_, ok2 := c.Get("key2")
	if !ok2 {
		t.Error("key2 不应被驱逐")
	}
}

func TestLRUCache_Delete(t *testing.T) {
	c := NewLRUCache(WithMaxSize(10))
	c.Set("key1", "value1")
	c.Delete("key1")

	_, ok := c.Get("key1")
	if ok {
		t.Error("删除后不应存在")
	}
}

func TestLRUCache_WithTTL(t *testing.T) {
	c := NewLRUCache(WithMaxSize(10), WithTTL(50*time.Millisecond))
	c.Set("key1", "value1")

	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("TTL 未过期应能获取")
	}
	if val != "value1" {
		t.Errorf("值应为 value1，实际为 %v", val)
	}

	// 等待过期
	time.Sleep(60 * time.Millisecond)
	_, ok = c.Get("key1")
	if ok {
		t.Error("TTL 过期后不应存在")
	}
}

func TestLRUCache_Clear(t *testing.T) {
	c := NewLRUCache(WithMaxSize(10))
	c.Set("key1", "val1")
	c.Set("key2", "val2")
	c.Clear()

	if _, ok := c.Get("key1"); ok {
		t.Error("Clear 后不应有数据")
	}
}

func TestLRUCache_Stats(t *testing.T) {
	c := NewLRUCache(WithMaxSize(10))
	c.Set("key1", "val1")
	c.Get("key1")
	c.Set("key2", "val2")
	c.Get("nonexistent")

	stats := c.Stats()
	if stats.Hits != 1 {
		t.Errorf("Hits 应为 1，实际为 %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses 应为 1，实际为 %d", stats.Misses)
	}
}

func TestNewLRUCacheWithTTL(t *testing.T) {
	c := NewLRUCacheWithTTL(10, 100*time.Millisecond)
	c.Set("key1", "val1")
	_, ok := c.Get("key1")
	if !ok {
		t.Error("应能获取刚设置的值")
	}
}
