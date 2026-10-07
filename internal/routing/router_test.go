package routing

import (
	"testing"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/reputation"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/warmup"
)

func newRouter() *Router {
	rep := reputation.NewStore()
	rep.Seed("ipA", "gmail", 92)
	rep.Seed("ipA", "outlook", 61)
	rep.Seed("ipB", "gmail", 78)
	rep.Seed("ipB", "outlook", 94)
	ips := []IP{{Addr: "ipA", Pool: PoolShared}, {Addr: "ipB", Pool: PoolShared}}
	return New(ips, rep, warmup.NewEngine(warmup.Thresholds{}))
}

func TestReputationAwareRouting(t *testing.T) {
	r := newRouter()
	if ip, _ := r.Select("c1", "gmail"); ip.Addr != "ipA" {
		t.Fatalf("gmail -> %s, want ipA", ip.Addr)
	}
	if ip, _ := r.Select("c1", "outlook"); ip.Addr != "ipB" {
		t.Fatalf("outlook -> %s, want ipB", ip.Addr)
	}
}

func TestDedicatedAffinity(t *testing.T) {
	rep := reputation.NewStore()
	rep.Seed("shared", "gmail", 90)
	rep.Seed("ded", "gmail", 85)
	ips := []IP{
		{Addr: "shared", Pool: PoolShared},
		{Addr: "ded", Pool: PoolDedicated, CustomerID: "acme"},
	}
	r := New(ips, rep, warmup.NewEngine(warmup.Thresholds{}))
	// dedicated gets +10 affinity => 95 beats shared 90
	if ip, _ := r.Select("acme", "gmail"); ip.Addr != "ded" {
		t.Fatalf("acme -> %s, want ded (affinity)", ip.Addr)
	}
	// a different customer cannot use acme's dedicated IP
	if ip, _ := r.Select("other", "gmail"); ip.Addr != "shared" {
		t.Fatalf("other -> %s, want shared", ip.Addr)
	}
}

func TestNoRouteError(t *testing.T) {
	r := New(nil, reputation.NewStore(), warmup.NewEngine(warmup.Thresholds{}))
	if _, err := r.Select("c1", "gmail"); err != ErrNoRoute {
		t.Fatalf("err = %v, want ErrNoRoute", err)
	}
}
