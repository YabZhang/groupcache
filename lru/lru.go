/*
Copyright 2013 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package lru implements an LRU cache.
package lru

import "container/list"

// Cache is an LRU cache. It is not safe for concurrent access.
// Cache 是一个基于 LRU（最近最少使用）算法的缓存实现。
// 注意：它不是并发安全的，外部使用（如 groupcache 中的 cache 结构体）必须自己加锁。
type Cache struct {
	// MaxEntries is the maximum number of cache entries before
	// an item is evicted. Zero means no limit.
	// 缓存最大条目数。如果是 0，代表没有限制。
	MaxEntries int

	// OnEvicted optionally specifies a callback function to be
	// executed when an entry is purged from the cache.
	// 可选的回调函数，当一个条目被驱逐（Evicted）时调用。
	// groupcache 利用这个回调来动态维护当前缓存占用的总字节数（减去被淘汰的键值对大小）。
	OnEvicted func(key Key, value interface{})

	// 双向链表，用于维护元素的访问顺序。
	// 链表头部（Front）是最近访问的，尾部（Back）是最久未访问的（即最容易被淘汰的）。
	ll    *list.List
	// 字典，用于快速查找某个 Key 对应的链表节点。
	cache map[interface{}]*list.Element
}

// A Key may be any value that is comparable. See http://golang.org/ref/spec#Comparison_operators
// Key 可以是任何可比较（Comparable）的类型。
type Key interface{}

// entry 是存放在双向链表中的节点数据。
// 必须同时保存 key 和 value。
// 为什么需要保存 key？因为当链表尾部节点被淘汰时，我们需要用这个 key 从字典（map）中删除对应的记录。
type entry struct {
	key   Key
	value interface{}
}

// New creates a new Cache.
// If maxEntries is zero, the cache has no limit and it's assumed
// that eviction is done by the caller.
func New(maxEntries int) *Cache {
	return &Cache{
		MaxEntries: maxEntries,
		ll:         list.New(),
		cache:      make(map[interface{}]*list.Element),
	}
}

// Add adds a value to the cache.
// 添加一个值到缓存中。
func (c *Cache) Add(key Key, value interface{}) {
	if c.cache == nil {
		c.cache = make(map[interface{}]*list.Element)
		c.ll = list.New()
	}
	// 1. 如果 Key 已经存在，则更新它的值，并将其移动到链表头部（表示最近刚被使用）
	if ee, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ee)
		ee.Value.(*entry).value = value
		return
	}
	// 2. 如果 Key 不存在，将新节点添加到链表头部
	ele := c.ll.PushFront(&entry{key, value})
	// 并在字典中记录这个新节点
	c.cache[key] = ele

	// 3. 检查是否超过了最大容量。如果超过，移除最老的节点（链表尾部）
	if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
		c.RemoveOldest()
	}
}

// Get looks up a key's value from the cache.
// 从缓存中查找一个值。
func (c *Cache) Get(key Key) (value interface{}, ok bool) {
	if c.cache == nil {
		return
	}
	// 如果在字典中找到了对应的节点
	if ele, hit := c.cache[key]; hit {
		// 每次访问后，将该节点移动到链表头部（维持 LRU 特性）
		c.ll.MoveToFront(ele)
		return ele.Value.(*entry).value, true
	}
	return
}

// Remove removes the provided key from the cache.
func (c *Cache) Remove(key Key) {
	if c.cache == nil {
		return
	}
	if ele, hit := c.cache[key]; hit {
		c.removeElement(ele)
	}
}

// RemoveOldest removes the oldest item from the cache.
// 移除缓存中最老的条目（链表尾部）。
// 这是 LRU 核心：空间不足时淘汰最近最少使用的数据。
func (c *Cache) RemoveOldest() {
	if c.cache == nil {
		return
	}
	// 拿到链表最后一个节点
	ele := c.ll.Back()
	if ele != nil {
		c.removeElement(ele)
	}
}

// removeElement 是移除节点的内部方法。
func (c *Cache) removeElement(e *list.Element) {
	// 从双向链表中删除
	c.ll.Remove(e)
	kv := e.Value.(*entry)
	// 从字典中删除对应的映射
	delete(c.cache, kv.key)
	// 如果注册了回调函数，则执行。
	// groupcache 通过这个机制减去被淘汰对象的 bytes()
	if c.OnEvicted != nil {
		c.OnEvicted(kv.key, kv.value)
	}
}

// Len returns the number of items in the cache.
func (c *Cache) Len() int {
	if c.cache == nil {
		return 0
	}
	return c.ll.Len()
}

// Clear purges all stored items from the cache.
func (c *Cache) Clear() {
	if c.OnEvicted != nil {
		for _, e := range c.cache {
			kv := e.Value.(*entry)
			c.OnEvicted(kv.key, kv.value)
		}
	}
	c.ll = nil
	c.cache = nil
}
