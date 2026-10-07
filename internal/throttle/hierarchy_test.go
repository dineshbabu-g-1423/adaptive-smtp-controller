package throttle

import (
	"testing"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

func testCfg() Config {
	return Config{
		GlobalRate: 500, GlobalBurst: 500, CustomerRate: 500, DomainRate: 200,
		DefaultProv: 80, ProviderRate: map[string]float64{"outlook": 100}, IPRate: 80,
	}
}

func TestEffectiveRateIsMinimum(t *testing.T) {
	h := NewHierarchy(testCfg())
	m := model.Message{CustomerID: "c1", Domain: "d.io", Provider: "outlook", AssignedIP: "1.1.1.1"}
	// min(500,500,200,100,80) = 80
	if got := h.EffectiveRate(m); got != 80 {
		t.Fatalf("effective rate = %v, want 80", got)
	}
}

func TestThrottleReducesProviderRate(t *testing.T) {
	h := NewHierarchy(testCfg())
	p := h.Provider("outlook")
	start := p.Rate()
	p.Throttle(0.75)
	if p.Rate() >= start {
		t.Fatalf("rate not reduced: %v >= %v", p.Rate(), start)
	}
	if p.Rate() != start*0.75 {
		t.Fatalf("rate = %v, want %v", p.Rate(), start*0.75)
	}
}

func TestRecoverDoesNotExceedBase(t *testing.T) {
	h := NewHierarchy(testCfg())
	p := h.Provider("outlook")
	p.Throttle(0.5)
	for i := 0; i < 1000; i++ {
		p.Recover(10)
	}
	if p.Rate() > p.BaseRate() {
		t.Fatalf("recovered above base: %v > %v", p.Rate(), p.BaseRate())
	}
}

func TestAdmitRejectsWhenEmpty(t *testing.T) {
	cfg := testCfg()
	cfg.GlobalRate, cfg.GlobalBurst = 1, 1
	h := NewHierarchy(cfg)
	m := model.Message{CustomerID: "c1", Domain: "d.io", Provider: "gmail"}
	if d := h.Admit(m); !d.Allowed {
		t.Fatal("first admit should pass")
	}
	if d := h.Admit(m); d.Allowed {
		t.Fatal("second admit should be rejected (bucket empty)")
	}
}
