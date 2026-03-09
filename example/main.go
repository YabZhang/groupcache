package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/golang/groupcache"
)

// simulateDB 模拟一个慢速数据库
var simulateDB = map[string][]byte{
	"Tom":  []byte("630"),
	"Jack": []byte("589"),
	"Sam":  []byte("567"),
}

func main() {
	var port int
	var api int
	flag.IntVar(&port, "port", 8001, "Geecache server port")
	flag.IntVar(&api, "api", 0, "API server port (to interact with)")
	flag.Parse()

	// 1. 初始化 Peer 地址
	// 我们假设有 3 个节点运行在 8001, 8002, 8003
	// 注意：groupcache 内部使用字符串完全匹配来查找 Peer
	addrMap := map[int]string{
		8001: "http://localhost:8001",
		8002: "http://localhost:8002",
		8003: "http://localhost:8003",
	}

	var peers []string
	for _, v := range addrMap {
		peers = append(peers, v)
	}

	// 2. 创建 Group
	// Group 是核心缓存命名空间
	group := groupcache.NewGroup("scores", 2<<20, groupcache.GetterFunc(
		func(ctx context.Context, key string, dest groupcache.Sink) error {
			log.Printf("[Server %d] Loading key \"%s\" from DB...", port, key)
			// 模拟慢速 DB 查询
			time.Sleep(100 * time.Millisecond)
			if v, ok := simulateDB[key]; ok {
				return dest.SetBytes(v)
			}
			return fmt.Errorf("key not found: %s", key)
		}))

	// 3. 启动 HTTP Peer
	// 这部分负责节点间的通信
	me := addrMap[port]
	pool := groupcache.NewHTTPPool(me)
	pool.Set(peers...)

	// 启动 Peer Server
	go func() {
		log.Printf("[Server %d] Peer server running at %s", port, me)
		// HTTPPool 实现了 http.Handler
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 如果请求是以 /_groupcache/ 开头，交给 pool 处理
			if len(r.URL.Path) >= len("/_groupcache/") && r.URL.Path[:13] == "/_groupcache/" {
				pool.ServeHTTP(w, r)
				return
			}
			// 否则这是 API 请求
			// 为了演示方便，我把 API 请求和 Peer 通信放在同一个端口处理，但是用不同的路径前缀区分
			// 在真实场景中，Peer 通信端口和 API 端口通常是分开的，或者用同一个 ServeMux 分发
			handleAPI(w, r, group)
		})))
	}()

	// 这里的 API server 只是为了方便演示，如果提供了 -api 参数，就会启动单独的端口
	// 否则我们在上面的 HandlerFunc 里处理 /api 请求
	if api > 0 {
		http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
			handleAPI(w, r, group)
		})
		log.Printf("[Server %d] API server running at http://localhost:%d/api", port, api)
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", api), nil))
	} else {
		select {}
	}
}

func handleAPI(w http.ResponseWriter, r *http.Request, group *groupcache.Group) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}

	log.Printf("[API] Requesting key \"%s\"", key)
	var data []byte
	// 调用 group.Get 获取数据
	err := group.Get(context.Background(), key, groupcache.AllocatingByteSliceSink(&data))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}
