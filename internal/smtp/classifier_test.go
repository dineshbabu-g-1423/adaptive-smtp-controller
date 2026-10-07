package smtp

import (
	"testing"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		code int
		want model.Outcome
	}{
		{250, model.OutcomeSuccess},
		{421, model.OutcomeDeferred},
		{451, model.OutcomeDeferred},
		{550, model.OutcomeBounce},
		{0, model.OutcomeTimeout},
	}
	for _, c := range cases {
		if got := Classify(c.code, ""); got != c.want {
			t.Errorf("Classify(%d) = %s, want %s", c.code, got, c.want)
		}
	}
}

func TestIsThrottleSignal(t *testing.T) {
	if !IsThrottleSignal(421, "4.7.650 Too many messages") {
		t.Error("421 should be a throttle signal")
	}
	if !IsThrottleSignal(250, "please try again later") {
		t.Error("text-based throttle should be detected")
	}
	if IsThrottleSignal(250, "OK queued") {
		t.Error("clean 250 is not a throttle signal")
	}
}
