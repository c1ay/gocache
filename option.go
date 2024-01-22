package gocache

import (
	"time"
)

// Option configures gocache.
type Option func(g *gocache)

// WithExpireAt returns an Option that set the expire
func WithExpireAt(d time.Duration) Option {
	return func(g *gocache) {
		g.Expire = d.Nanoseconds()
	}
}
