// Package circuitbreaker contains blast-radius containment per provider route.
//
// A provider route moves through states based on recent outcome health:
//
//	HEALTHY -> DEGRADED -> THROTTLED -> OPEN
//
// When OPEN we stop aggressive delivery to that provider but retain its
// messages for later retry; other providers keep flowing unaffected.
package circuitbreaker

import (
	"sync"
	"time"
)

// State is the circuit position for one provider route.
type State string

const (
	StateHealthy   State = "HEALTHY"
	StateDegraded  State = "DEGRADED"
	StateThrottled State = "THROTTLED"
	StateOpen      State = "OPEN"
)

// Breaker tracks a rolling failure ratio and derives a state from it.
type Breaker struct {
	mu          sync.Mutex
	state       State
	window      []bool // true = failure (4xx/5xx/timeout)
	size        int
	openedAt    time.Time
	cooldown    time.Duration
	degradeAt   float64 // failure ratio thresholds
	throttleAt  float64
	openAt      float64
	clock       func() time.Time
}

// New builds a breaker with a rolling window of the given size.
func New(windowSize int, cooldown time.Duration) *Breaker {
	if windowSize < 10 {
		windowSize = 10
	}
	return &Breaker{
		state:      StateHealthy,
		window:     make([]bool, 0, windowSize),
		size:       windowSize,
		cooldown:   cooldown,
		degradeAt:  0.15,
		throttleAt: 0.30,
		openAt:     0.50,
		clock:      time.Now,
	}
}

// Record ingests an outcome (failure=true) and recomputes state.
func (b *Breaker) Record(failure bool) State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.window = append(b.window, failure)
	if len(b.window) > b.size {
		b.window = b.window[1:]
	}
	b.evaluate()
	return b.state
}

func (b *Breaker) evaluate() {
	if len(b.window) < b.size/2 {
		return // not enough signal yet
	}
	fails := 0
	for _, f := range b.window {
		if f {
			fails++
		}
	}
	ratio := float64(fails) / float64(len(b.window))

	prev := b.state
	switch {
	case ratio >= b.openAt:
		b.state = StateOpen
	case ratio >= b.throttleAt:
		b.state = StateThrottled
	case ratio >= b.degradeAt:
		b.state = StateDegraded
	default:
		b.state = StateHealthy
	}
	if b.state == StateOpen && prev != StateOpen {
		b.openedAt = b.clock()
	}
}

// Allow reports whether delivery to this route is currently permitted.
// When OPEN, it permits a single trial probe after the cooldown (half-open).
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != StateOpen {
		return true
	}
	if b.clock().Sub(b.openedAt) >= b.cooldown {
		// half-open probe: allow one attempt and reset the window a bit
		b.window = b.window[:0]
		b.state = StateThrottled
		return true
	}
	return false
}

// State returns the current state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// ProviderPressure maps the state to a retry-pressure multiplier.
func (b *Breaker) ProviderPressure() float64 {
	switch b.State() {
	case StateDegraded:
		return 2
	case StateThrottled:
		return 4
	case StateOpen:
		return 8
	default:
		return 1
	}
}
