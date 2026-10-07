package throttle

import (
	"fmt"
	"sync"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

// Decision explains whether a message may be sent, and which limit bound it.
type Decision struct {
	Allowed      bool
	Bottleneck   string  // name of the limiter that blocked (or would bind)
	EffectiveRate float64 // min() rate across the hierarchy
}

// Config captures the default rate ceilings for each dimension.
type Config struct {
	GlobalRate   float64            `yaml:"global_rate"`
	GlobalBurst  float64            `yaml:"global_burst"`
	CustomerRate float64            `yaml:"customer_rate"`
	DomainRate   float64            `yaml:"domain_rate"`
	ProviderRate map[string]float64 `yaml:"provider_rate"`
	DefaultProv  float64            `yaml:"default_provider_rate"`
	IPRate       float64            `yaml:"ip_rate"`
}

// Hierarchy owns all limiters and lazily creates per-key buckets.
type Hierarchy struct {
	mu   sync.Mutex
	cfg  Config
	glob *Limiter
	cust map[string]*Limiter // by customerID
	dom  map[string]*Limiter // by sender domain
	prov map[string]*Limiter // by destination provider
	ip   map[string]*Limiter // by IP address
}

// NewHierarchy constructs the five-level limiter tree from config.
func NewHierarchy(cfg Config) *Hierarchy {
	if cfg.GlobalBurst == 0 {
		cfg.GlobalBurst = cfg.GlobalRate
	}
	return &Hierarchy{
		cfg:  cfg,
		glob: NewLimiter("global", cfg.GlobalRate, cfg.GlobalBurst),
		cust: map[string]*Limiter{},
		dom:  map[string]*Limiter{},
		prov: map[string]*Limiter{},
		ip:   map[string]*Limiter{},
	}
}

func (h *Hierarchy) customer(id string) *Limiter {
	if l, ok := h.cust[id]; ok {
		return l
	}
	l := NewLimiter("customer:"+id, h.cfg.CustomerRate, h.cfg.CustomerRate)
	h.cust[id] = l
	return l
}

func (h *Hierarchy) domain(d string) *Limiter {
	if l, ok := h.dom[d]; ok {
		return l
	}
	l := NewLimiter("domain:"+d, h.cfg.DomainRate, h.cfg.DomainRate)
	h.dom[d] = l
	return l
}

func (h *Hierarchy) provider(p string) *Limiter {
	if l, ok := h.prov[p]; ok {
		return l
	}
	rate := h.cfg.DefaultProv
	if r, ok := h.cfg.ProviderRate[p]; ok {
		rate = r
	}
	l := NewLimiter("provider:"+p, rate, rate)
	h.prov[p] = l
	return l
}

// IP returns (creating if needed) the limiter for an IP address.
func (h *Hierarchy) IP(addr string) *Limiter {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ipLocked(addr)
}

func (h *Hierarchy) ipLocked(addr string) *Limiter {
	if l, ok := h.ip[addr]; ok {
		return l
	}
	l := NewLimiter("ip:"+addr, h.cfg.IPRate, h.cfg.IPRate)
	h.ip[addr] = l
	return l
}

// Provider returns the provider limiter (exposed for the circuit breaker).
func (h *Hierarchy) Provider(p string) *Limiter {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.provider(p)
}

// EffectiveRate computes min() across all dimensions without consuming a token.
func (h *Hierarchy) EffectiveRate(m model.Message) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	levels := h.levels(m)
	min := levels[0].rate
	for _, lv := range levels[1:] {
		if lv.rate < min {
			min = lv.rate
		}
	}
	return min
}

type level struct {
	lim  *Limiter
	rate float64
}

func (h *Hierarchy) levels(m model.Message) []level {
	ipName := m.AssignedIP
	if ipName == "" {
		ipName = "unassigned"
	}
	ls := []level{
		{h.glob, h.glob.Rate()},
		{h.customer(m.CustomerID), h.customer(m.CustomerID).Rate()},
		{h.domain(m.Domain), h.domain(m.Domain).Rate()},
		{h.provider(m.Provider), h.provider(m.Provider).Rate()},
	}
	if m.AssignedIP != "" {
		l := h.ipLocked(ipName)
		ls = append(ls, level{l, l.Rate()})
	}
	return ls
}

// Admit tries to consume one token at every level. If any level is out of
// tokens the message is rejected and no tokens are consumed anywhere.
func (h *Hierarchy) Admit(m model.Message) Decision {
	h.mu.Lock()
	defer h.mu.Unlock()

	levels := h.levels(m)
	eff := levels[0].rate
	for _, lv := range levels[1:] {
		if lv.rate < eff {
			eff = lv.rate
		}
	}

	// two-phase: check all, then consume all (atomic under h.mu via Allow order)
	granted := make([]*Limiter, 0, len(levels))
	for _, lv := range levels {
		if lv.lim.Allow() {
			granted = append(granted, lv.lim)
		} else {
			// roll back already-granted tokens
			for _, g := range granted {
				g.giveBack()
			}
			return Decision{Allowed: false, Bottleneck: lv.lim.Name(), EffectiveRate: eff}
		}
	}
	return Decision{Allowed: true, Bottleneck: "", EffectiveRate: eff}
}

// Snapshot returns a human-readable view of all current rates.
func (h *Hierarchy) Snapshot() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]string{}
	out[h.glob.Name()] = fmt.Sprintf("%.0f/%.0f", h.glob.Rate(), h.glob.BaseRate())
	for _, l := range h.prov {
		out[l.Name()] = fmt.Sprintf("%.0f/%.0f", l.Rate(), l.BaseRate())
	}
	for _, l := range h.ip {
		out[l.Name()] = fmt.Sprintf("%.0f/%.0f", l.Rate(), l.BaseRate())
	}
	return out
}

// giveBack returns one token (used for atomic multi-level admission rollback).
func (l *Limiter) giveBack() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.tokens+1 < l.burst {
		l.tokens++
	} else {
		l.tokens = l.burst
	}
}
