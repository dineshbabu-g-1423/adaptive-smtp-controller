package circuitbreaker

import (
	"testing"
	"time"
)

func TestOpensUnderHighFailure(t *testing.T) {
	b := New(20, time.Second)
	for i := 0; i < 20; i++ {
		b.Record(true) // all failures
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %s, want OPEN", b.State())
	}
	if b.Allow() {
		t.Fatal("OPEN breaker should block immediately")
	}
}

func TestStaysHealthyUnderLowFailure(t *testing.T) {
	b := New(20, time.Second)
	for i := 0; i < 20; i++ {
		b.Record(false)
	}
	if b.State() != StateHealthy {
		t.Fatalf("state = %s, want HEALTHY", b.State())
	}
}

func TestHalfOpenProbeAfterCooldown(t *testing.T) {
	b := New(20, 10*time.Millisecond)
	for i := 0; i < 20; i++ {
		b.Record(true)
	}
	if b.State() != StateOpen {
		t.Fatalf("expected OPEN, got %s", b.State())
	}
	time.Sleep(15 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("should allow a half-open probe after cooldown")
	}
}

func TestProviderPressureScalesWithState(t *testing.T) {
	b := New(20, time.Second)
	if b.ProviderPressure() != 1 {
		t.Fatalf("healthy pressure = %v, want 1", b.ProviderPressure())
	}
	for i := 0; i < 20; i++ {
		b.Record(true)
	}
	if b.ProviderPressure() <= 1 {
		t.Fatalf("open pressure should be > 1, got %v", b.ProviderPressure())
	}
}
