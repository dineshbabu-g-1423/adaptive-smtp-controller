package simulator

import (
	"sync"
	"testing"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

// The simulator is probabilistic, so these use large samples and loose bounds.
// A flaky test in a repo people read is worse than no test at all.
const samples = 20000

func rates(f *Fleet, provider string) (ok, deferred, bounced, throttled float64) {
	msg := model.Message{Provider: provider}
	var o, d, b, th int
	for i := 0; i < samples; i++ {
		r := f.Send(msg, "192.0.2.1")
		switch {
		case r.Code == 250:
			o++
		case r.Code == 421:
			d++
			th++
		case r.Code == 451:
			d++
		case r.Code >= 500:
			b++
		}
	}
	n := float64(samples)
	return float64(o) / n, float64(d) / n, float64(b) / n, float64(th) / n
}

func TestNormalOperationMostlyDelivers(t *testing.T) {
	f := NewFleet()
	ok, deferred, bounced, _ := rates(f, "gmail")
	if ok < 0.90 {
		t.Errorf("success rate %.3f, expected above 0.90 under normal conditions", ok)
	}
	if deferred > 0.08 {
		t.Errorf("deferral rate %.3f, expected near the configured 0.02", deferred)
	}
	if bounced > 0.05 {
		t.Errorf("bounce rate %.3f, expected near the configured 0.01", bounced)
	}
}

// The incident switch is the demo's whole point: turning it on has to produce
// a visible storm of transient failures, not a slight uptick.
func TestThrottleIncidentProducesADeferralStorm(t *testing.T) {
	f := NewFleet()
	_, beforeDeferred, _, _ := rates(f, "gmail")

	f.SetThrottle("gmail", true)
	okDuring, duringDeferred, _, throttled := rates(f, "gmail")

	if duringDeferred < 0.50 {
		t.Errorf("deferral rate during the incident %.3f, expected above 0.50", duringDeferred)
	}
	if duringDeferred <= beforeDeferred*5 {
		t.Errorf("deferrals only went from %.3f to %.3f, expected a step change", beforeDeferred, duringDeferred)
	}
	if okDuring > 0.45 {
		t.Errorf("success rate during the incident %.3f, expected it to fall sharply", okDuring)
	}
	// During an incident the transient reply must be the rate-limit code, not
	// the generic one: downstream policy treats them differently.
	if throttled < duringDeferred*0.95 {
		t.Errorf("only %.3f of %.3f deferrals used 421, expected nearly all of them", throttled, duringDeferred)
	}
}

func TestThrottleCanBeClearedAgain(t *testing.T) {
	f := NewFleet()
	f.SetThrottle("yahoo", true)
	_, during, _, _ := rates(f, "yahoo")
	f.SetThrottle("yahoo", false)
	_, after, _, _ := rates(f, "yahoo")

	if during < 0.50 {
		t.Fatalf("incident deferral rate %.3f, expected above 0.50", during)
	}
	if after > 0.10 {
		t.Errorf("deferral rate after recovery %.3f, expected a return to normal", after)
	}
}

// An incident on one provider must not change another's behaviour, or the
// blast-radius demo proves nothing.
func TestIncidentIsScopedToOneProvider(t *testing.T) {
	f := NewFleet()
	f.SetThrottle("gmail", true)

	_, gmailDeferred, _, _ := rates(f, "gmail")
	okOutlook, outlookDeferred, _, _ := rates(f, "outlook")

	if gmailDeferred < 0.50 {
		t.Fatalf("gmail deferral rate %.3f, expected the incident to bite", gmailDeferred)
	}
	if outlookDeferred > 0.10 {
		t.Errorf("outlook deferral rate %.3f while gmail is down, expected it unaffected", outlookDeferred)
	}
	if okOutlook < 0.85 {
		t.Errorf("outlook success rate %.3f, expected it to keep flowing", okOutlook)
	}
}

func TestUnknownProviderFallsBackInsteadOfFailing(t *testing.T) {
	f := NewFleet()
	ok, _, _, _ := rates(f, "some-other-host.example")
	if ok < 0.85 {
		t.Errorf("success rate for an unconfigured provider %.3f, expected the permissive default", ok)
	}
	// Must not panic.
	f.SetThrottle("some-other-host.example", true)
}

func TestConcurrentSendIsSafe(t *testing.T) {
	f := NewFleet()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				f.Send(model.Message{Provider: "gmail"}, "192.0.2.1")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			f.SetThrottle("gmail", j%2 == 0)
		}
	}()
	wg.Wait()
}
