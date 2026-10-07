// Package smtp contains the delivery transport abstraction and the
// outcome classifier that turns SMTP responses into delivery outcomes.
package smtp

import (
	"strings"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

// Classify maps an SMTP reply code/text to a delivery Outcome.
//
//	2xx          -> success
//	4xx          -> deferred (transient; retry)
//	5xx          -> bounce   (permanent; drop)
//	code == 0    -> timeout  (no response)
func Classify(code int, text string) model.Outcome {
	switch {
	case code == 0:
		return model.OutcomeTimeout
	case code >= 200 && code < 300:
		return model.OutcomeSuccess
	case code >= 400 && code < 500:
		return model.OutcomeDeferred
	case code >= 500 && code < 600:
		return model.OutcomeBounce
	default:
		return model.OutcomeDeferred
	}
}

// IsThrottleSignal detects provider rate-limiting responses that should
// trigger dynamic rate reduction (e.g. Outlook "421 4.7.650 Too many messages").
func IsThrottleSignal(code int, text string) bool {
	if code == 421 || code == 450 || code == 451 {
		return true
	}
	t := strings.ToLower(text)
	for _, needle := range []string{
		"too many", "rate limit", "throttl", "try again later",
		"4.7.650", "temporarily deferred", "too much mail",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}
