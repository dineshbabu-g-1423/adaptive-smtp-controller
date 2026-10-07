// Package warmup models IP warmup schedules with automatic rollback.
//
// Each dedicated IP ramps its daily allowance over several days. The engine
// does NOT blindly increase: if bounce/deferral/complaint rates exceed limits
// it pauses the warmup or rolls back to the previous day's ceiling.
package warmup

import (
	"sync"
	"time"
)

// Schedule is the per-day cap ladder for one IP.
type Schedule struct {
	IP   string    `yaml:"ip"`
	Days []float64 `yaml:"days"` // Days[0] = day1 cap, etc.
}

// Thresholds beyond which warmup is paused or rolled back.
type Thresholds struct {
	BounceRate   float64 `yaml:"bounce_rate"`   // e.g. 0.05
	DeferralRate float64 `yaml:"deferral_rate"` // e.g. 0.20
	ComplaintRate float64 `yaml:"complaint_rate"` // e.g. 0.01
}

// IPState tracks progress of one IP through its warmup schedule.
type IPState struct {
	sched     Schedule
	dayIndex  int
	paused    bool
	startedAt time.Time
}

// Engine coordinates all warming IPs.
type Engine struct {
	mu   sync.Mutex
	ips  map[string]*IPState
	th   Thresholds
	clock func() time.Time
}

// NewEngine builds a warmup engine with the given health thresholds.
func NewEngine(th Thresholds) *Engine {
	return &Engine{ips: map[string]*IPState{}, th: th, clock: time.Now}
}

// Register starts warming an IP on day 1.
func (e *Engine) Register(s Schedule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ips[s.IP] = &IPState{sched: s, dayIndex: 0, startedAt: e.clock()}
}

// Cap returns the current per-second warmup cap for an IP.
// IPs not under warmup return 0, meaning "no warmup overlay".
func (e *Engine) Cap(ip string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.ips[ip]
	if !ok {
		return 0
	}
	daily := st.sched.Days[st.dayIndex]
	// convert a daily allowance to a smoothed per-second ceiling
	return daily / 86400
}

// IsWarming reports if the IP is still in its ramp.
func (e *Engine) IsWarming(ip string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.ips[ip]
	return ok && st.dayIndex < len(st.sched.Days)-1
}

// Advance attempts to move an IP to the next warmup day. It consults live
// health rates and will pause or roll back instead of advancing when unhealthy.
func (e *Engine) Advance(ip string, bounce, deferral, complaint float64) Action {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.ips[ip]
	if !ok {
		return Action{IP: ip, Kind: ActionNone}
	}

	switch {
	case complaint > e.th.ComplaintRate || bounce > e.th.BounceRate:
		if st.dayIndex > 0 {
			st.dayIndex--
			return Action{IP: ip, Kind: ActionRollback, Day: st.dayIndex + 1,
				NewCap: st.sched.Days[st.dayIndex]}
		}
		st.paused = true
		return Action{IP: ip, Kind: ActionPause, Day: st.dayIndex + 1}
	case deferral > e.th.DeferralRate:
		st.paused = true
		return Action{IP: ip, Kind: ActionPause, Day: st.dayIndex + 1}
	default:
		st.paused = false
		if st.dayIndex < len(st.sched.Days)-1 {
			st.dayIndex++
			return Action{IP: ip, Kind: ActionAdvance, Day: st.dayIndex + 1,
				NewCap: st.sched.Days[st.dayIndex]}
		}
		return Action{IP: ip, Kind: ActionComplete, Day: st.dayIndex + 1}
	}
}

// ActionKind enumerates warmup decisions.
type ActionKind string

const (
	ActionNone     ActionKind = "none"
	ActionAdvance  ActionKind = "advance"
	ActionPause    ActionKind = "pause"
	ActionRollback ActionKind = "rollback"
	ActionComplete ActionKind = "complete"
)

// Action is the result of an Advance call.
type Action struct {
	IP     string
	Kind   ActionKind
	Day    int
	NewCap float64
}
