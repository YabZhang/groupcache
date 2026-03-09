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

// Package consistenthash provides an implementation of a ring hash.
package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
)

type Hash func(data []byte) uint32

// Map 是一致性哈希的主数据结构。
type Map struct {
	hash     Hash
	replicas int    // 虚拟节点倍数
	keys     []int  // 哈希环（有序的虚拟节点哈希值列表）
	hashMap  map[int]string // 虚拟节点哈希值 -> 真实节点名称的映射
}

func New(replicas int, fn Hash) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

// IsEmpty returns true if there are no items available.
func (m *Map) IsEmpty() bool {
	return len(m.keys) == 0
}

// Add 方法添加新的节点到哈希环中。
// keys 是真实节点的名称（例如 IP:Port）。
func (m *Map) Add(keys ...string) {
	for _, key := range keys {
		// 为每个真实节点创建 m.replicas 个虚拟节点
		for i := 0; i < m.replicas; i++ {
			// 计算虚拟节点的哈希值：hash(i + key)
			hash := int(m.hash([]byte(strconv.Itoa(i) + key)))
			m.keys = append(m.keys, hash)
			// 记录虚拟节点到真实节点的映射
			m.hashMap[hash] = key
		}
	}
	// 对所有虚拟节点的哈希值进行排序，形成环
	sort.Ints(m.keys)
}

// Get 根据 key 获取最近的节点（Owner）。
func (m *Map) Get(key string) string {
	if m.IsEmpty() {
		return ""
	}

	hash := int(m.hash([]byte(key)))

	// 二分查找，找到第一个哈希值 >= key哈希值 的虚拟节点
	idx := sort.Search(len(m.keys), func(i int) bool { return m.keys[i] >= hash })

	// 如果找不到（idx == len），说明 key 的哈希值比环上所有节点都大，
	// 根据环状特性，回绕到第一个节点。
	if idx == len(m.keys) {
		idx = 0
	}

	// 返回对应的真实节点
	return m.hashMap[m.keys[idx]]
}
