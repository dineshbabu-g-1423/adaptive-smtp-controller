package scheduler

import (
	"testing"
	"time"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/circuitbreaker"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/reputation"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/retry"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/routing"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/smtp"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/throttle"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/warmup"
)

// stubTransport always returns the same reply.
type stubTransport struct{ reply smtp.Reply }

func (s stubTransport) Send(model.Message, string) smtp.Reply { return s.reply }

func build(reply smtp.Reply) *Scheduler {
	cfg := throttle.Config{
		GlobalRate: 1000, GlobalBurst: 1000, CustomerRate: 1000, DomainRate: 1000,
		DefaultProv: 1000, IPRate: 1000,
	}
	rep := reputation.NewStore()
	router := routing.New([]routing.IP{{Addr: "ip1", Pool: routing.PoolShared}},
		rep, warmup.NewEngine(warmup.Thresholds{}))
	return New(Deps{
		Queue: NewQueue(), Hier: throttle.NewHierarchy(cfg), Router: router,
		Rep: rep, Transport: stubTransport{reply}, Policy: retry.DefaultPolicy(),
		Breakers: map[string]*circuitbreaker.Breaker{},
	})
}

func msg() model.Message {
	return model.Message{ID: "m", CustomerID: "c", Domain: "d.io", Provider: "gmail",
		EnqueuedAt: time.Now(), NextAttempt: time.Now()}
}

func TestSuccessfulDelivery(t *testing.T) {
	s := build(smtp.Reply{Code: 250, Text: "OK"})
	s.deps.Queue.Enqueue(msg())
	s.drainOnce(time.Now())
	if s.Stats().Sent != 1 {
		t.Fatalf("sent = %d, want 1", s.Stats().Sent)
	}
}

func TestDeferredIsRequeued(t *testing.T) {
	s := build(smtp.Reply{Code: 451, Text: "deferred"})
	s.deps.Queue.Enqueue(msg())
	s.drainOnce(time.Now())
	if s.Stats().Deferred != 1 {
		t.Fatalf("deferred = %d, want 1", s.Stats().Deferred)
	}
	if s.deps.Queue.Depth() != 1 {
		t.Fatalf("queue depth = %d, want 1 (requeued)", s.deps.Queue.Depth())
	}
}

func TestBounceIsDropped(t *testing.T) {
	s := build(smtp.Reply{Code: 550, Text: "rejected"})
	s.deps.Queue.Enqueue(msg())
	s.drainOnce(time.Now())
	if s.Stats().Bounced != 1 {
		t.Fatalf("bounced = %d, want 1", s.Stats().Bounced)
	}
	if s.deps.Queue.Depth() != 0 {
		t.Fatalf("queue depth = %d, want 0 (dropped)", s.deps.Queue.Depth())
	}
}

func TestThrottleSignalReducesProviderRate(t *testing.T) {
	s := build(smtp.Reply{Code: 421, Text: "4.7.650 too many"})
	base := s.deps.Hier.Provider("gmail").Rate()
	s.deps.Queue.Enqueue(msg())
	s.drainOnce(time.Now())
	if s.deps.Hier.Provider("gmail").Rate() >= base {
		t.Fatalf("provider rate not reduced after 421")
	}
}
