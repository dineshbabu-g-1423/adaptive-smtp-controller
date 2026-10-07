// Package reputation maintains per-(IP, provider) reputation scores derived
// from live delivery signals, and powers reputation-aware route selection.
package reputation

import (
	"sync"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

// Score is a 0..100 reputation value for an (IP, provider) pair.
type key struct {
	ip       string
	provider string
}

// Store holds reputation scores and updates them via EWMA on outcomes.
type Store struct {
	mu     sync.RWMutex
	scores map[key]float64
	alpha  float64 // EWMA smoothing factor
}

// NewStore returns an empty store. Unknown pairs start neutral (70).
func NewStore() *Store {
	return &Store{scores: map[key]float64{}, alpha: 0.1}
}

const neutral = 70.0

// Get returns the current score for (ip, provider), defaulting to neutral.
func (s *Store) Get(ip, provider string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.scores[key{ip, provider}]; ok {
		return v
	}
	return neutral
}

// Seed sets an initial score (used from config for demos).
func (s *Store) Seed(ip, provider string, score float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scores[key{ip, provider}] = clamp(score)
}

// Observe folds a delivery outcome into the score via EWMA.
func (s *Store) Observe(a model.Attempt) {
	var target float64
	switch a.Outcome {
	case model.OutcomeSuccess:
		target = 100
	case model.OutcomeDeferred:
		target = 55
	case model.OutcomeTimeout:
		target = 40
	case model.OutcomeBounce:
		target = 10
	default:
		target = neutral
	}
	k := key{a.IP, a.Provider}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.scores[k]
	if !ok {
		cur = neutral
	}
	s.scores[k] = clamp(cur*(1-s.alpha) + target*s.alpha)
}

// Snapshot returns all scores keyed as "ip|provider".
func (s *Store) Snapshot() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]float64, len(s.scores))
	for k, v := range s.scores {
		out[k.ip+"|"+k.provider] = v
	}
	return out
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
