package warmup

import "testing"

func engine() *Engine {
	return NewEngine(Thresholds{BounceRate: 0.05, DeferralRate: 0.20, ComplaintRate: 0.01})
}

func TestAdvanceWhenHealthy(t *testing.T) {
	e := engine()
	e.Register(Schedule{IP: "ip1", Days: []float64{1000, 2500, 5000}})
	a := e.Advance("ip1", 0.01, 0.05, 0.001)
	if a.Kind != ActionAdvance || a.Day != 2 {
		t.Fatalf("got %+v, want advance to day 2", a)
	}
}

func TestRollbackOnHighBounce(t *testing.T) {
	e := engine()
	e.Register(Schedule{IP: "ip1", Days: []float64{1000, 2500, 5000}})
	e.Advance("ip1", 0, 0, 0) // -> day 2
	a := e.Advance("ip1", 0.10, 0, 0)
	if a.Kind != ActionRollback || a.Day != 1 {
		t.Fatalf("got %+v, want rollback to day 1", a)
	}
}

func TestPauseOnHighDeferral(t *testing.T) {
	e := engine()
	e.Register(Schedule{IP: "ip1", Days: []float64{1000, 2500}})
	a := e.Advance("ip1", 0, 0.5, 0)
	if a.Kind != ActionPause {
		t.Fatalf("got %+v, want pause", a)
	}
}

func TestCapConvertsDailyToPerSecond(t *testing.T) {
	e := engine()
	e.Register(Schedule{IP: "ip1", Days: []float64{86400}})
	if cap := e.Cap("ip1"); cap != 1 {
		t.Fatalf("cap = %v, want 1/sec", cap)
	}
	if e.Cap("unknown") != 0 {
		t.Fatal("unknown IP should have no warmup overlay")
	}
}
