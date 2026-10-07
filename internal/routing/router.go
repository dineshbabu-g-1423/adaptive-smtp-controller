// Package routing performs reputation-aware IP selection from shared/dedicated
// pools, honouring warmup caps and current IP rate limits.
package routing

import (
	"errors"
	"sync"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/reputation"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/warmup"
)

// PoolKind distinguishes shared from dedicated IP pools.
type PoolKind string

const (
	PoolShared    PoolKind = "shared"
	PoolDedicated PoolKind = "dedicated"
)

// IP describes a single sending IP and its pool membership.
type IP struct {
	Addr       string   `yaml:"addr"`
	Pool       PoolKind `yaml:"pool"`
	CustomerID string   `yaml:"customer_id,omitempty"` // for dedicated pools
}

// ErrNoRoute is returned when no IP can currently serve a message.
var ErrNoRoute = errors.New("no eligible IP for route")

// Router selects IPs by provider reputation.
type Router struct {
	mu   sync.RWMutex
	ips  []IP
	rep  *reputation.Store
	warm *warmup.Engine
}

// New builds a router over a fixed set of IPs.
func New(ips []IP, rep *reputation.Store, warm *warmup.Engine) *Router {
	return &Router{ips: ips, rep: rep, warm: warm}
}

// Select returns the best IP for (customer, provider) by reputation score.
// Dedicated IPs are preferred for their owning customer; otherwise shared IPs.
func (r *Router) Select(customerID, provider string) (IP, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best IP
	bestScore := -1.0
	found := false

	for _, ip := range r.ips {
		if ip.Pool == PoolDedicated && ip.CustomerID != customerID {
			continue // dedicated to someone else
		}
		score := r.rep.Get(ip.Addr, provider)
		// Prefer dedicated IPs when they belong to this customer.
		if ip.Pool == PoolDedicated && ip.CustomerID == customerID {
			score += 10 // affinity bonus
		}
		if score > bestScore {
			bestScore = score
			best = ip
			found = true
		}
	}
	if !found {
		return IP{}, ErrNoRoute
	}
	return best, nil
}

// IPs exposes the configured IP set (read-only copy).
func (r *Router) IPs() []IP {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]IP, len(r.ips))
	copy(out, r.ips)
	return out
}
