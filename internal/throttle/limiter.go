// Package throttle implements hierarchical, dynamically-adjustable rate limiting.
//
// The effective send rate for a message is min() across five dimensions:
//
//	Global -> Customer -> Domain -> Provider -> IP (and warmup overlays the IP)
//
// Each limiter is a token-bucket whose rate can be reduced on the fly when a
// destination starts throttling us, and gradually recovered afterwards.
package throttle

import (
	"math"
	"sync"
	"time"
)

// Limiter is a thread-safe token bucket with an adjustable rate.
type Limiter struct {
	mu         sync.Mutex
	name       string
	baseRate   float64 // configured ceiling (tokens/sec)
	rate       float64 // current effective rate (tokens/sec), <= baseRate
	burst      float64 // max tokens held
	tokens     float64
	last       time.Time
	minRate    float64 // floor we never drop below
	clock      func() time.Time
}

// NewLimiter builds a limiter running at ratePerSec with a burst allowance.
func NewLimiter(name string, ratePerSec, burst float64) *Limiter {
	if burst <= 0 {
		burst = math.Max(1, ratePerSec)
	}
	return &Limiter{
		name:     name,
		baseRate: ratePerSec,
		rate:     ratePerSec,
		burst:    burst,
		tokens:   burst,
		last:     time.Now(),
		minRate:  1,
		clock:    time.Now,
	}
}

// Allow returns true if one token is available, consuming it if so.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

func (l *Limiter) refill() {
	now := l.clock()
	elapsed := now.Sub(l.last).Seconds()
	if elapsed <= 0 {
		return
	}
	l.tokens = math.Min(l.burst, l.tokens+elapsed*l.rate)
	l.last = now
}

// Throttle multiplicatively reduces the current rate (e.g. factor 0.75 => -25%).
// Used when a provider/domain/IP signals it is overloaded (4xx storms).
func (l *Limiter) Throttle(factor float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if factor <= 0 || factor >= 1 {
		return
	}
	l.rate = math.Max(l.minRate, l.rate*factor)
	l.clampBurst()
}

// Recover additively nudges the rate back toward the configured base.
func (l *Limiter) Recover(step float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if step <= 0 {
		return
	}
	l.rate = math.Min(l.baseRate, l.rate+step)
	l.clampBurst()
}

// SetRate pins the current rate to an explicit value (used by the warmup engine).
func (l *Limiter) SetRate(r float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = math.Max(0, math.Min(l.baseRate, r))
	l.clampBurst()
}

func (l *Limiter) clampBurst() {
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
}

// Rate returns the current effective rate.
func (l *Limiter) Rate() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

// BaseRate returns the configured ceiling.
func (l *Limiter) BaseRate() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.baseRate
}

// Name identifies the limiter in diagnostics.
func (l *Limiter) Name() string { return l.name }
