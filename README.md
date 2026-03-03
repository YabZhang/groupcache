# groupcache

## Summary

groupcache is a distributed caching and cache-filling library, intended as a
replacement for a pool of memcached nodes in many cases.

For API docs and examples, see http://godoc.org/github.com/golang/groupcache

## Comparison to memcached

### **Like memcached**, groupcache:

 * shards by key to select which peer is responsible for that key

### **Unlike memcached**, groupcache:

 * does not require running a separate set of servers, thus massively
   reducing deployment/configuration pain.  groupcache is a client
   library as well as a server.  It connects to its own peers, forming
   a distributed cache.

 * comes with a cache filling mechanism.  Whereas memcached just says
   "Sorry, cache miss", often resulting in a thundering herd of
   database (or whatever) loads from an unbounded number of clients
   (which has resulted in several fun outages), groupcache coordinates
   cache fills such that only one load in one process of an entire
   replicated set of processes populates the cache, then multiplexes
   the loaded value to all callers.

 * does not support versioned values.  If key "foo" is value "bar",
   key "foo" must always be "bar".  There are neither cache expiration
   times, nor explicit cache evictions.  Thus there is also no CAS,
   nor Increment/Decrement.  This also means that groupcache....

 * ... supports automatic mirroring of super-hot items to multiple
   processes.  This prevents memcached hot spotting where a machine's
   CPU and/or NIC are overloaded by very popular keys/values.

 * is currently only available for Go.  It's very unlikely that I
   (bradfitz@) will port the code to any other language.

## Loading process

In a nutshell, a groupcache lookup of **Get("foo")** looks like:

(On machine #5 of a set of N machines running the same code)

 1. Is the value of "foo" in local memory because it's super hot?  If so, use it.

 2. Is the value of "foo" in local memory because peer #5 (the current
    peer) is the owner of it?  If so, use it.

 3. Amongst all the peers in my set of N, am I the owner of the key
    "foo"?  (e.g. does it consistent hash to 5?)  If so, load it.  If
    other callers come in, via the same process or via RPC requests
    from peers, they block waiting for the load to finish and get the
    same answer.  If not, RPC to the peer that's the owner and get
    the answer.  If the RPC fails, just load it locally (still with
    local dup suppression).

## Design and Implementation

### Architecture Overview

groupcache is structured as a library that each application process embeds.
All participating processes form a peer group and communicate over HTTP using
Protocol Buffers for serialization.  There is no separate cache daemon; each
process is both a client and a server.

```
┌──────────────────────────────────────────────────────────┐
│                   groupcache package                     │
│                                                          │
│  ┌──────────┐   ┌───────────┐   ┌────────────────────┐  │
│  │  Group   │──▶│  cache    │──▶│  lru.Cache         │  │
│  │(namespace)│  │(mainCache)│   │  (LRU eviction)    │  │
│  │          │  └───────────┘   └────────────────────┘  │
│  │          │   ┌───────────┐                           │
│  │          │──▶│  cache    │  (hotCache – popular      │
│  │          │   │(hotCache) │   keys mirrored locally)  │
│  │          │   └───────────┘                           │
│  │          │   ┌──────────────┐                        │
│  │          │──▶│singleflight  │  (dedup concurrent     │
│  │          │   │  .Group      │   loads per key)       │
│  └──────────┘   └──────────────┘                        │
│       │                                                  │
│  ┌────▼──────────────────────────────────────────────┐  │
│  │                 HTTPPool / PeerPicker              │  │
│  │          (consistent hash → peer URL)             │  │
│  └───────────────────────────┬───────────────────────┘  │
└──────────────────────────────┼───────────────────────────┘
                               │ HTTP GET  (protobuf body)
                    ┌──────────▼──────────┐
                    │  Remote peer process │
                    │  (same binary,       │
                    │   same library)      │
                    └─────────────────────┘
```

### Package Structure

| Package | File(s) | Responsibility |
|---|---|---|
| `groupcache` | `groupcache.go` | Core `Group` type, `Get` logic, dual-cache management, stats |
| `groupcache` | `peers.go` | `PeerPicker` / `ProtoGetter` interfaces, peer registration |
| `groupcache` | `http.go` | `HTTPPool` — HTTP server + client for inter-peer RPC |
| `groupcache` | `byteview.go` | Immutable `ByteView` value type wrapping `[]byte` or `string` |
| `groupcache` | `sinks.go` | `Sink` interface and implementations for receiving cache values |
| `lru` | `lru/lru.go` | Generic LRU cache backed by a doubly-linked list + hash map |
| `consistenthash` | `consistenthash/consistenthash.go` | Ring-hash for mapping keys to peer URLs |
| `singleflight` | `singleflight/singleflight.go` | Deduplicates concurrent in-flight calls for the same key |
| `groupcachepb` | `groupcachepb/` | Protocol Buffer definitions for `GetRequest` / `GetResponse` |

### Core Data Structures

#### `Group` (`groupcache.go`)

A `Group` is a named cache namespace backed by a user-supplied `Getter`
(the origin data source).  Each process can host multiple groups.

```
Group
├── name        string           – unique namespace identifier
├── getter      Getter           – callback to load a missing key from origin
├── peers       PeerPicker       – selects the authoritative peer for a key
├── cacheBytes  int64            – combined byte budget for both caches
├── mainCache   cache            – keys owned by this peer (via consistent hash)
├── hotCache    cache            – popular keys mirrored from other peers
├── loadGroup   singleflight.Group – deduplicates concurrent loads
└── Stats       Stats            – atomic counters for observability
```

#### `cache` (internal, `groupcache.go`)

A thread-safe wrapper around `lru.Cache` that stores `ByteView` values and
tracks byte usage for budget enforcement.

#### `ByteView` (`byteview.go`)

An immutable, value-type view of a byte sequence.  It stores data as either
`[]byte` or `string` to avoid unnecessary allocations.  All values returned
from the cache are `ByteView`s — callers cannot mutate cached data.

#### `Sink` (`sinks.go`)

`Sink` is the interface through which a `Getter` writes a loaded value back
into groupcache.  Multiple concrete implementations are provided:

| Sink | Description |
|---|---|
| `StringSink` | Populates a `*string` |
| `ByteViewSink` | Populates a `*ByteView` (zero-copy fast path for cache hits) |
| `AllocatingByteSliceSink` | Allocates and populates a `*[]byte` |
| `TruncatingByteSliceSink` | Writes into an existing `[]byte`, truncating if needed |
| `ProtoSink` | Unmarshals a protobuf message |

### Key Algorithms

#### Consistent Hashing (`consistenthash/`)

`HTTPPool` uses a consistent hash ring to map any cache key to exactly one
peer URL.  Each peer is placed on the ring at `replicas` (default 50)
virtual positions so that load is spread evenly and key ownership shifts
minimally when peers are added or removed.

```
Add("10.0.0.1:8080"):
  hash(0+"10.0.0.1:8080"), hash(1+"10.0.0.1:8080"), …  →  sorted ring

Get("mykey"):
  h = hash("mykey")
  binary-search ring for first position ≥ h  →  owner URL
```

#### Singleflight (`singleflight/`)

When multiple goroutines request the same key simultaneously and none is
cached, `singleflight.Group.Do` ensures that only **one** actual load is
performed.  All other callers block on a `sync.WaitGroup` and receive the
same result once the single in-flight call completes.  This completely
eliminates thundering-herd load spikes for popular keys.

#### Dual-Cache with Budget Enforcement (`groupcache.go`)

Each `Group` maintains two LRU caches sharing a single byte budget
(`cacheBytes`):

* **`mainCache`** — holds keys for which *this* peer is the consistent-hash
  owner.  Values are written here after a successful local load.
* **`hotCache`** — holds keys owned by *other* peers that were fetched over
  the network but are popular enough to cache locally too.  A value is
  promoted to `hotCache` randomly (~10 % of remote fetches) to avoid
  network hotspots without wasting too much local memory.

When the combined size exceeds `cacheBytes`, the eviction loop removes the
oldest entry from `hotCache` first (as long as `hotCache > mainCache / 8`),
otherwise from `mainCache`.

### Request Lifecycle

```
caller → Group.Get(ctx, key, sink)
            │
            ├─ lookupCache(key)  →  mainCache hit?  →  return ByteView
            │                    →  hotCache hit?   →  return ByteView
            │
            └─ load(key)  [wrapped in singleflight.Do]
                  │
                  ├─ lookupCache again (double-check after singleflight dedup)
                  │
                  ├─ peers.PickPeer(key)
                  │       │
                  │       ├─ peer != self  →  httpGetter.Get(peer, key)
                  │       │                       │
                  │       │                       └─ HTTP GET /_groupcache/{group}/{key}
                  │       │                          ← protobuf GetResponse
                  │       │                          maybe populate hotCache (~10%)
                  │       │
                  │       └─ peer == self (or RPC error)
                  │                   │
                  │                   └─ getter.Get(ctx, key, sink)  [user callback]
                  │                      populate mainCache
                  │
                  └─ return ByteView to all waiting callers
```

### Peer Communication (`http.go`)

`HTTPPool` serves two roles simultaneously:

1. **HTTP server** — registers `/_groupcache/` with `http.DefaultServeMux`.
   Incoming requests have the form `/_groupcache/{groupName}/{key}`.
   The handler calls `Group.Get`, serializes the result as a protobuf
   `GetResponse`, and writes it to the response body.

2. **HTTP client** — each known peer URL is wrapped in an `httpGetter`.
   Outbound requests are plain `GET` calls; responses are deserialized from
   protobuf.  A `sync.Pool` of `bytes.Buffer` objects reduces allocations on
   the hot path.

### Observability

`Group.Stats` exposes atomic counters that can be read at any time:

| Counter | Meaning |
|---|---|
| `Gets` | Total `Get` calls received (local + from peers) |
| `CacheHits` | Served directly from mainCache or hotCache |
| `Loads` | Cache misses that required a load |
| `LoadsDeduped` | Loads after singleflight deduplication |
| `PeerLoads` | Successful fetches from a remote peer |
| `PeerErrors` | Failed fetches from a remote peer |
| `LocalLoads` | Successful loads via the user `Getter` |
| `LocalLoadErrs` | Failed loads via the user `Getter` |
| `ServerRequests` | Requests received *as* a peer from another node |

### Quick Start

```go
import "github.com/golang/groupcache"

// 1. Register this process in the peer pool.
pool := groupcache.NewHTTPPool("http://10.0.0.1:8080")
pool.Set("http://10.0.0.1:8080", "http://10.0.0.2:8080", "http://10.0.0.3:8080")

// 2. Define a Group with a Getter that loads from the origin.
var g = groupcache.NewGroup("users", 64<<20 /* 64 MB */, groupcache.GetterFunc(
    func(ctx context.Context, key string, dest groupcache.Sink) error {
        data, err := db.Lookup(key)
        if err != nil {
            return err
        }
        return dest.SetBytes(data)
    },
))

// 3. Get a value (cache-fill happens automatically on miss).
var data []byte
err := g.Get(ctx, "user:42", groupcache.AllocatingByteSliceSink(&data))
```

## Users

groupcache is in production use by dl.google.com (its original user),
parts of Blogger, parts of Google Code, parts of Google Fiber, parts
of Google production monitoring systems, etc.

## Presentations

See http://talks.golang.org/2013/oscon-dl.slide

## Help

Use the golang-nuts mailing list for any discussion or questions.
