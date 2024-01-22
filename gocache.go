package gocache

import (
	"sync"
	"time"

	"github.com/cespare/xxhash"
	"github.com/kpango/fastime"
)

// Gocache is base gocache interface.
type Gocache interface {

	// Get returns object with the given name from the cache.
	Get(string) (any, bool)

	// Set sets object in the cache.
	Set(string, any) bool

	// SetWithExpire sets object in cache with an expiration date.
	SetWithExpire(string, any, time.Duration) bool

	// Delete deletes cache object of given name.
	Delete(string)

	// Clear clears cache.
	Clear()

	// StartExpired starts worker that deletes an expired cache object.
	// Deletion processing is executed at intervals of given time.
	StartExpired(dur time.Duration) Gocache

	// StopExpired stop worker that deletes an expired cache object.
	StopExpired() Gocache
}

type (
	gocache struct {
		shards shards
		*config
	}

	shard struct {
		*sync.Map
		starting bool
		doneCh   chan struct{}
	}

	shards []*shard

	record struct {
		val    any
		expire int64
	}
)

func (r *record) isValid() bool {
	return fastime.Now().UnixNano() < r.expire
}

// New returns Gocache (*gocache) instance.
func New(options ...Option) Gocache {
	g := newDefaultGocache()

	for _, opt := range options {
		opt(g)
	}

	for i := 0; i < int(g.ShardsCount); i++ {
		g.shards[i] = newDefaultShard()
	}

	return g
}

func newDefaultGocache() *gocache {
	return &gocache{
		shards: make(shards, DefaultShardsCount),
		config: newDefaultConfig(),
	}
}

func newDefaultShard() *shard {
	return &shard{
		Map:      new(sync.Map),
		starting: false,
		doneCh:   make(chan struct{}),
	}
}

func (g *gocache) getShard(key string) *shard {
	return g.shards[xxhash.Sum64([]byte(key))%g.ShardsCount]
}

func (g *gocache) Get(key string) (any, bool) {
	val, ok := g.getShard(key).get(key)
	if !ok {
		return nil, false
	}

	return val, ok
}

func (g *gocache) Set(key string, val any) bool {
	shard := g.getShard(key)
	return shard.set(key, val, g.Expire)
}

func (g *gocache) SetWithExpire(key string, val any, expire time.Duration) bool {
	shard := g.getShard(key)
	return shard.set(key, val, int64(expire.Nanoseconds()))
}

func (g *gocache) Delete(key string) {
	g.getShard(key).delete(key)
}

func (g *gocache) DeleteExpired() {
	for _, shard := range g.shards {
		shard.deleteExpired()
	}
}

func (g *gocache) Clear() {
	for _, shard := range g.shards {
		shard.deleteAll()
	}
}

func (g *gocache) StartExpired(dur time.Duration) Gocache {
	if dur <= 0 {
		return g
	}

	for _, shard := range g.shards {
		if shard.starting {
			return g
		}
	}

	for _, shard := range g.shards {
		shard.starting = true
		go shard.start(dur)
	}

	return g
}

func (g *gocache) StopExpired() Gocache {
	for _, shard := range g.shards {
		if shard.starting {
			shard.doneCh <- struct{}{}
			shard.starting = false
		}
	}

	return g
}

func (s *shard) get(key string) (any, bool) {
	val, ok := s.Load(key)
	if !ok {
		return nil, false
	}

	rcd := val.(*record)
	if rcd.isValid() {
		return rcd.val, ok
	}

	s.Delete(key)

	return nil, false
}

func (s *shard) set(key string, val any, expire int64) bool {
	if expire <= 0 {
		return false
	}

	s.Store(key, &record{
		val:    val,
		expire: fastime.Now().UnixNano() + expire,
	})

	return true
}

func (s *shard) delete(key string) {
	s.Delete(key)
}

func (s *shard) deleteAll() {
	s.Range(func(key any, val any) bool {
		s.Delete(key)
		return true
	})
}

func (s *shard) deleteExpired() {
	s.Range(func(key any, val any) bool {
		if !val.(*record).isValid() {
			s.Delete(key)
		}
		return true
	})
}

func (s *shard) start(dur time.Duration) {
	t := time.NewTicker(dur)
	defer func() {
		t.Stop()
		close(s.doneCh)
	}()

	for {
		select {
		case _ = <-s.doneCh:
			return
		case _ = <-t.C:
			s.deleteExpired()
		}
	}
}
