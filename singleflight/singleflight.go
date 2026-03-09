/*
Copyright 2012 Google Inc.

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

// Package singleflight provides a duplicate function call suppression
// mechanism.
package singleflight

import "sync"

// call 代表一个正在进行中（或已完成）的函数调用。
// 它包含一个 WaitGroup，用于让其他 goroutine 等待这个调用完成。
type call struct {
	wg  sync.WaitGroup
	val interface{}
	err error
}

// Group 是 singleflight 的核心，用于管理不同 key 的飞行中（in-flight）请求。
type Group struct {
	mu sync.Mutex       // 保护 m
	m  map[string]*call // 懒加载，存储 key -> call 的映射
}

// Do 执行给定的函数 fn，并确保对于给定的 key，同一时间只有一个 fn 在执行。
// 如果有重复的请求进来，它们会等待原来的请求完成，并返回相同的结果。
// 这是防止缓存击穿的关键机制。
func (g *Group) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	// 如果 map 中已经存在该 key，说明已经有请求在处理了。
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		// 等待原来的请求完成（通过 WaitGroup）
		c.wg.Wait()
		// 返回原来的请求结果
		return c.val, c.err
	}

	// 如果没有，创建一个新的 call
	c := new(call)
	c.wg.Add(1) // 计数器 +1
	g.m[key] = c // 放入 map，标记该 key 正在处理
	g.mu.Unlock()

	// 执行真正的函数（例如去数据库加载数据）
	c.val, c.err = fn()
	c.wg.Done() // 计数器 -1，通知所有等待的 goroutine

	g.mu.Lock()
	delete(g.m, key) // 处理完成，从 map 中移除
	g.mu.Unlock()

	return c.val, c.err
}
