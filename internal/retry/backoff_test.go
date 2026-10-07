package retry

import (
	"testing"
	"time"
)

func TestDelayGrowsWithDefers(t *testing.T) {
	p := DefaultPolicy()
	p.Jitter = 0 // deterministic
	low := p.Delay(1, Pressure{Queue: 1, Provider: 1})
	high := p.Delay(4, Pressure{Queue: 1, Provider: 1})
	if high <= low {
		t.Fatalf("delay did not grow: %v <= %v", high, low)
	}
}

func TestPressureIncreasesDelay(t *testing.T) {
	p := DefaultPolicy()
	p.Jitter = 0
	calm := p.Delay(2, Pressure{Queue: 1, Provider: 1})
	storm := p.Delay(2, Pressure{Queue: 5, Provider: 8})
	if storm <= calm {
		t.Fatalf("pressure did not increase delay: %v <= %v", storm, calm)
	}
}

func TestDelayCappedAtMax(t *testing.T) {
	p := DefaultPolicy()
	p.Jitter = 0
	d := p.Delay(50, Pressure{Queue: 100, Provider: 100})
	if d > p.Max {
		t.Fatalf("delay exceeded max: %v > %v", d, p.Max)
	}
}

func TestShouldBounce(t *testing.T) {
	p := DefaultPolicy()
	if !p.ShouldBounce(p.MaxDefers+1, time.Minute) {
		t.Fatal("should bounce after max defers")
	}
	if !p.ShouldBounce(1, p.Deadline+time.Hour) {
		t.Fatal("should bounce after deadline")
	}
	if p.ShouldBounce(1, time.Minute) {
		t.Fatal("should not bounce early")
	}
}

func TestQueuePressureMonotonic(t *testing.T) {
	if QueuePressure(100) >= QueuePressure(10000) {
		t.Fatal("queue pressure should increase with depth")
	}
}
