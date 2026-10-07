// Package retry computes queue-aware retry delays instead of a flat interval.
//
// retry_delay = base_backoff * f(defer_count) * queue_pressure * provider_pressure
//
// The goal is to avoid retry storms: as the queue deepens or a provider
// degrades, deferred messages wait longer, relieving backpressure.
package retry

import (
	"math"
	"math/rand"
	"time"
)

// Policy parameterises the backoff computation.
type Policy struct {
	Base       time.Duration // base backoff unit
	Max        time.Duration // ceiling on any single delay
	Deadline   time.Duration // give up (bounce) after this total age
	Jitter     float64       // 0..1 fraction of randomised jitter
	MaxDefers  int           // hard cap on defer attempts
}

// DefaultPolicy returns sensible defaults.
func DefaultPolicy() Policy {
	return Policy{
		Base:      30 * time.Second,
		Max:       2 * time.Hour,
		Deadline:  48 * time.Hour,
		Jitter:    0.2,
		MaxDefers: 12,
	}
}

// Pressure describes transient system/provider load on a 1.0+ scale.
type Pressure struct {
	Queue    float64 // 1.0 = light, grows with queue depth
	Provider float64 // 1.0 = healthy, grows as provider degrades
}

// Delay computes the next retry delay for a message deferred deferCount times.
func (p Policy) Delay(deferCount int, pr Pressure) time.Duration {
	if deferCount < 1 {
		deferCount = 1
	}
	q := clampLow(pr.Queue, 1)
	pv := clampLow(pr.Provider, 1)

	// exponential-ish growth in defer_count, scaled by live pressure
	d := float64(p.Base) * math.Pow(2, float64(deferCount-1)) * q * pv
	if d > float64(p.Max) {
		d = float64(p.Max)
	}
	if p.Jitter > 0 {
		j := 1 + (rand.Float64()*2-1)*p.Jitter // [1-jitter, 1+jitter]
		d *= j
	}
	return time.Duration(d)
}

// ShouldBounce decides if a deferred message has exhausted its budget.
func (p Policy) ShouldBounce(deferCount int, age time.Duration) bool {
	if deferCount > p.MaxDefers {
		return true
	}
	return age >= p.Deadline
}

// QueuePressure maps a queue depth to a multiplier (saturating curve).
func QueuePressure(depth int) float64 {
	// 0 -> 1.0, 10k -> ~5x, saturating.
	return 1 + 4*(1-math.Exp(-float64(depth)/4000))
}

func clampLow(v, low float64) float64 {
	if v < low {
		return low
	}
	return v
}
