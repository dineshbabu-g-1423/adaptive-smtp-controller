// Package scheduler is the delivery control plane: it dequeues ready messages,
// selects a route, enforces hierarchical throttling and circuit breaking,
// classifies outcomes, and reschedules deferrals with queue-aware backoff.
package scheduler

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/circuitbreaker"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/reputation"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/retry"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/routing"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/smtp"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/throttle"
)

// Stats is an atomic counter bundle for observability.
type Stats struct {
	Sent      int64
	Deferred  int64
	Bounced   int64
	Timeouts  int64
	Blocked   int64 // permanent refusals aimed at the sender, not the recipient
	Throttled int64 // admission rejections (rate limited)
}

// Deps are the collaborators the scheduler orchestrates.
type Deps struct {
	Queue     *Queue
	Hier      *throttle.Hierarchy
	Router    *routing.Router
	Rep       *reputation.Store
	Transport smtp.Transport
	Policy    retry.Policy
	Breakers  map[string]*circuitbreaker.Breaker // by provider
}

// Scheduler runs the control loop.
type Scheduler struct {
	deps    Deps
	stats   Stats
	mu      sync.Mutex
	recent  []model.Attempt // ring buffer for diagnostics
	breaker func(string) *circuitbreaker.Breaker
	stop    chan struct{}
}

// New constructs a scheduler. Breakers are created lazily per provider.
func New(d Deps) *Scheduler {
	if d.Breakers == nil {
		d.Breakers = map[string]*circuitbreaker.Breaker{}
	}
	s := &Scheduler{deps: d, stop: make(chan struct{})}
	s.breaker = func(p string) *circuitbreaker.Breaker {
		s.mu.Lock()
		defer s.mu.Unlock()
		b, ok := d.Breakers[p]
		if !ok {
			b = circuitbreaker.New(50, 15*time.Second)
			d.Breakers[p] = b
		}
		return b
	}
	return s
}

// Run drives the control loop at the given tick interval until Stop.
func (s *Scheduler) Run(tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.drainOnce(time.Now())
		}
	}
}

// Stop halts the control loop.
func (s *Scheduler) Stop() { close(s.stop) }

// drainOnce processes as many ready messages as the limiters permit this tick.
func (s *Scheduler) drainOnce(now time.Time) {
	for i := 0; i < 1000; i++ { // bounded work per tick
		m, ok := s.deps.Queue.PopReady(now)
		if !ok {
			return
		}
		s.process(m, now)
	}
}

// process handles a single message end-to-end.
func (s *Scheduler) process(m model.Message, now time.Time) {
	// 1. circuit breaker: if provider route is OPEN, hold message for retry.
	br := s.breaker(m.Provider)
	if !br.Allow() {
		s.requeue(m, br, now)
		return
	}

	// 2. route selection (reputation-aware).
	ip, err := s.deps.Router.Select(m.CustomerID, m.Provider)
	if err == nil {
		m.AssignedIP = ip.Addr
	}

	// 3. hierarchical admission (min across all five levels).
	dec := s.deps.Hier.Admit(m)
	if !dec.Allowed {
		atomic.AddInt64(&s.stats.Throttled, 1)
		// rate-limited: push back a little without counting as a deferral
		m.NextAttempt = now.Add(250 * time.Millisecond)
		s.deps.Queue.Enqueue(m)
		return
	}

	// 4. deliver via transport.
	start := time.Now()
	reply := s.deps.Transport.Send(m, m.AssignedIP)
	outcome := smtp.Classify(reply.Code, reply.Text)

	att := model.Attempt{
		MessageID: m.ID, Provider: m.Provider, IP: m.AssignedIP,
		Outcome: outcome, SMTPCode: reply.Code, SMTPText: reply.Text,
		At: now, LatencyMS: time.Since(start).Milliseconds(), DeferCount: m.DeferCount,
	}
	s.record(att)
	s.deps.Rep.Observe(att)

	// 5. react to throttle signals by dynamically reducing provider+IP rates.
	if smtp.IsThrottleSignal(reply.Code, reply.Text) {
		s.deps.Hier.Provider(m.Provider).Throttle(0.75)
		if m.AssignedIP != "" {
			s.deps.Hier.IP(m.AssignedIP).Throttle(0.75)
		}
	}

	// 6. update breaker + route the outcome.
	failure := outcome != model.OutcomeSuccess
	br.Record(failure)

	switch outcome {
	case model.OutcomeSuccess:
		atomic.AddInt64(&s.stats.Sent, 1)
		// healthy traffic: let provider rate recover a touch.
		s.deps.Hier.Provider(m.Provider).Recover(1)
	case model.OutcomeBounce:
		// Permanent and the recipient's fault: drop the message. In a system
		// with a suppression list this is where the address would be added.
		atomic.AddInt64(&s.stats.Bounced, 1)
	case model.OutcomeBlocked:
		// Permanent and our fault. Drop the message, but the recipient is fine
		// and must not be suppressed. Back the provider off: continuing at the
		// same rate into a policy refusal is how a soft block becomes a hard one.
		atomic.AddInt64(&s.stats.Blocked, 1)
		s.deps.Hier.Provider(m.Provider).Throttle(0.75)
	case model.OutcomeTimeout:
		atomic.AddInt64(&s.stats.Timeouts, 1)
		s.requeue(m, br, now)
	case model.OutcomeDeferred:
		atomic.AddInt64(&s.stats.Deferred, 1)
		s.requeue(m, br, now)
	}
}

// requeue reschedules a message with queue-aware backoff, or bounces it when
// the retry budget is exhausted.
func (s *Scheduler) requeue(m model.Message, br *circuitbreaker.Breaker, now time.Time) {
	m.DeferCount++
	age := now.Sub(m.EnqueuedAt)
	if s.deps.Policy.ShouldBounce(m.DeferCount, age) {
		atomic.AddInt64(&s.stats.Bounced, 1)
		return
	}
	pr := retry.Pressure{
		Queue:    retry.QueuePressure(s.deps.Queue.Depth()),
		Provider: br.ProviderPressure(),
	}
	delay := s.deps.Policy.Delay(m.DeferCount, pr)
	m.NextAttempt = now.Add(delay)
	s.deps.Queue.Enqueue(m)
}

func (s *Scheduler) record(a model.Attempt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent = append(s.recent, a)
	if len(s.recent) > 500 {
		s.recent = s.recent[len(s.recent)-500:]
	}
}

// Stats returns a snapshot of the counters.
func (s *Scheduler) Stats() Stats {
	return Stats{
		Sent:      atomic.LoadInt64(&s.stats.Sent),
		Deferred:  atomic.LoadInt64(&s.stats.Deferred),
		Bounced:   atomic.LoadInt64(&s.stats.Bounced),
		Timeouts:  atomic.LoadInt64(&s.stats.Timeouts),
		Blocked:   atomic.LoadInt64(&s.stats.Blocked),
		Throttled: atomic.LoadInt64(&s.stats.Throttled),
	}
}

// RecentAttempts returns a copy of the diagnostics ring buffer.
func (s *Scheduler) RecentAttempts() []model.Attempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Attempt, len(s.recent))
	copy(out, s.recent)
	return out
}

// BreakerStates returns each provider's current circuit state.
func (s *Scheduler) BreakerStates() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for p, b := range s.deps.Breakers {
		out[p] = string(b.State())
	}
	return out
}
