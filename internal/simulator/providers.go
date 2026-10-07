// Package simulator fakes mailbox providers (Gmail/Outlook/Yahoo) so the
// control plane can be exercised deterministically without sending real email.
package simulator

import (
	"math/rand"
	"sync"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/smtp"
)

// ProviderSim models one destination provider's behaviour.
type ProviderSim struct {
	Name         string
	baseDeferral float64 // probability of a 4xx under normal load
	baseBounce   float64 // probability of a 5xx
	throttling   bool    // incident toggle
}

// Fleet is the set of simulated providers plus an incident switch.
type Fleet struct {
	mu   sync.Mutex
	sims map[string]*ProviderSim
}

// NewFleet creates default Gmail/Outlook/Yahoo simulators.
func NewFleet() *Fleet {
	return &Fleet{sims: map[string]*ProviderSim{
		"gmail":   {Name: "gmail", baseDeferral: 0.02, baseBounce: 0.01},
		"outlook": {Name: "outlook", baseDeferral: 0.03, baseBounce: 0.01},
		"yahoo":   {Name: "yahoo", baseDeferral: 0.02, baseBounce: 0.015},
	}}
}

// SetThrottle toggles an incident on a provider (the demo entry point).
func (f *Fleet) SetThrottle(provider string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sims[provider]; ok {
		s.throttling = on
	}
}

// Send implements smtp.Transport: returns a simulated SMTP reply.
func (f *Fleet) Send(m model.Message, ip string) smtp.Reply {
	f.mu.Lock()
	s, ok := f.sims[m.Provider]
	throttling := ok && s.throttling
	deferral := 0.02
	bounce := 0.01
	if ok {
		deferral, bounce = s.baseDeferral, s.baseBounce
	}
	f.mu.Unlock()

	if throttling {
		deferral += 0.60 // storm of 421s during the incident
	}

	r := rand.Float64()
	switch {
	case r < bounce:
		return smtp.Reply{Code: 550, Text: "5.1.1 recipient rejected"}
	case r < bounce+deferral:
		if throttling {
			return smtp.Reply{Code: 421, Text: "4.7.650 Too many messages from this IP"}
		}
		return smtp.Reply{Code: 451, Text: "4.3.0 temporarily deferred, try again later"}
	default:
		return smtp.Reply{Code: 250, Text: "2.0.0 OK queued"}
	}
}
