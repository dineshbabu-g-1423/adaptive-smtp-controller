package reputation

import (
	"math"
	"sync"
	"testing"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

func observe(s *Store, ip, provider string, o model.Outcome, n int) {
	for i := 0; i < n; i++ {
		s.Observe(model.Attempt{IP: ip, Provider: provider, Outcome: o})
	}
}

func TestUnknownPairIsNeutral(t *testing.T) {
	s := NewStore()
	if got := s.Get("192.0.2.1", "gmail"); got != neutral {
		t.Fatalf("Get on an unseen pair = %v, want %v", got, neutral)
	}
}

func TestOutcomesMoveTheScoreInTheRightDirection(t *testing.T) {
	cases := []struct {
		outcome model.Outcome
		up      bool
	}{
		{model.OutcomeSuccess, true},
		{model.OutcomeDeferred, false},
		{model.OutcomeTimeout, false},
		{model.OutcomeBounce, false},
	}
	for _, c := range cases {
		s := NewStore()
		s.Observe(model.Attempt{IP: "ip", Provider: "gmail", Outcome: c.outcome})
		got := s.Get("ip", "gmail")
		if c.up && got <= neutral {
			t.Errorf("%s: score %v, expected a rise above %v", c.outcome, got, neutral)
		}
		if !c.up && got >= neutral {
			t.Errorf("%s: score %v, expected a fall below %v", c.outcome, got, neutral)
		}
	}
}

// The point of smoothing: one bad attempt against an otherwise healthy address
// must not collapse its score. A sender that reroutes on a single 5xx will
// thrash between addresses and warm none of them.
func TestOneBounceDoesNotCollapseAGoodReputation(t *testing.T) {
	s := NewStore()
	observe(s, "ip", "gmail", model.OutcomeSuccess, 50)
	before := s.Get("ip", "gmail")
	if before < 95 {
		t.Fatalf("sustained success only reached %v, expected it to approach 100", before)
	}

	s.Observe(model.Attempt{IP: "ip", Provider: "gmail", Outcome: model.OutcomeBounce})
	after := s.Get("ip", "gmail")

	drop := before - after
	if drop > 15 {
		t.Errorf("a single bounce dropped the score by %v, which is too sharp to be stable", drop)
	}
	if after >= before {
		t.Error("a bounce must still move the score down")
	}
}

// Sustained failure has to be able to reach a low score, or the smoothing
// would make the signal useless.
func TestSustainedFailureReachesALowScore(t *testing.T) {
	s := NewStore()
	observe(s, "ip", "gmail", model.OutcomeBounce, 100)
	if got := s.Get("ip", "gmail"); got > 15 {
		t.Fatalf("100 bounces left the score at %v, expected it to approach 10", got)
	}
}

// Reputation is a property of a pair, not of an address. An IP burned at one
// provider is often fine at another, and collapsing the two would strand
// healthy capacity.
func TestReputationIsPerProviderNotPerIP(t *testing.T) {
	s := NewStore()
	observe(s, "ip", "gmail", model.OutcomeBounce, 40)

	if gmail := s.Get("ip", "gmail"); gmail > 30 {
		t.Fatalf("gmail score %v, expected it to have fallen", gmail)
	}
	if yahoo := s.Get("ip", "yahoo"); yahoo != neutral {
		t.Fatalf("yahoo score %v, expected the untouched pair to stay neutral at %v", yahoo, neutral)
	}
}

func TestScoreStaysWithinBounds(t *testing.T) {
	s := NewStore()
	observe(s, "ip", "gmail", model.OutcomeSuccess, 500)
	if v := s.Get("ip", "gmail"); v > 100 {
		t.Errorf("score %v exceeded 100", v)
	}
	observe(s, "ip2", "gmail", model.OutcomeBounce, 500)
	if v := s.Get("ip2", "gmail"); v < 0 {
		t.Errorf("score %v fell below 0", v)
	}
	s.Seed("ip3", "gmail", 1000)
	if v := s.Get("ip3", "gmail"); v != 100 {
		t.Errorf("Seed clamped to %v, want 100", v)
	}
	s.Seed("ip4", "gmail", -50)
	if v := s.Get("ip4", "gmail"); v != 0 {
		t.Errorf("Seed clamped to %v, want 0", v)
	}
}

func TestSeedThenObserveMovesFromTheSeededValue(t *testing.T) {
	s := NewStore()
	s.Seed("ip", "gmail", 20)
	s.Observe(model.Attempt{IP: "ip", Provider: "gmail", Outcome: model.OutcomeSuccess})
	got := s.Get("ip", "gmail")
	if got <= 20 || got > 30 {
		t.Fatalf("score %v: expected a small rise from the seeded 20, not a jump", got)
	}
}

func TestSnapshotKeysBothDimensions(t *testing.T) {
	s := NewStore()
	s.Seed("192.0.2.1", "gmail", 80)
	s.Seed("192.0.2.1", "yahoo", 40)
	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot has %d entries, want 2", len(snap))
	}
	if math.Abs(snap["192.0.2.1|gmail"]-80) > 0.001 {
		t.Errorf("gmail entry = %v, want 80", snap["192.0.2.1|gmail"])
	}
	if math.Abs(snap["192.0.2.1|yahoo"]-40) > 0.001 {
		t.Errorf("yahoo entry = %v, want 40", snap["192.0.2.1|yahoo"])
	}
}

// Delivery workers observe outcomes concurrently. Run under -race.
func TestConcurrentObserveIsSafe(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Observe(model.Attempt{IP: "ip", Provider: "gmail", Outcome: model.OutcomeSuccess})
				_ = s.Get("ip", "gmail")
				_ = s.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	if v := s.Get("ip", "gmail"); v < neutral {
		t.Fatalf("after 1600 successes the score is %v, expected it above neutral", v)
	}
}
