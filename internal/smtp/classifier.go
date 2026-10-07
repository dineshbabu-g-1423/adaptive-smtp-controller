// Package smtp contains the delivery transport abstraction and the
// outcome classifier that turns SMTP responses into delivery outcomes.
package smtp

import (
	"regexp"
	"strings"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

// enhancedRe matches an RFC 3463 enhanced status code at a token boundary.
var enhancedRe = regexp.MustCompile(`\b([245])\.(\d{1,3})\.(\d{1,3})\b`)

// senderFaultSubjects are the RFC 3463 subject.detail pairs that describe a
// problem with the sender rather than the recipient. 7.x is policy and
// security, extended by RFC 7372 for authentication failures.
var senderFaultSubjects = map[string]bool{
	"7.0": true, "7.1": true, "7.2": true, "7.4": true, "7.5": true,
	"7.6": true, "7.7": true, "7.20": true, "7.21": true, "7.22": true,
	"7.23": true, "7.24": true, "7.25": true, "7.26": true,
}

// recipientFaultText is wording receivers use when the address itself is bad.
var recipientFaultText = []string{
	"user unknown", "no such user", "no such mailbox", "does not exist",
	"doesn't exist", "recipient address rejected", "unknown or illegal alias",
	"mailbox unavailable", "invalid recipient", "recipient not found",
}

// senderFaultText is wording receivers use when the sender is the problem.
var senderFaultText = []string{
	"spam", "unsolicited", "blocked", "blacklist", "blocklist", "reputation",
	"not authorized", "not authorised", "access denied", "policy", "refused",
	"spf", "dkim", "dmarc", "banned", "rejected due to",
}

// Classify maps an SMTP reply to a delivery Outcome.
//
//	2xx       -> success
//	4xx       -> deferred (transient; retry)
//	5xx       -> bounce   when the recipient is at fault
//	          -> blocked  when the sender is at fault
//	code == 0 -> timeout  (no response)
//
// The 5xx split matters because the two cases need opposite handling. A
// 550 naming an unknown mailbox means the address is dead and should never be
// tried again. A 550 citing reputation or policy means the address is perfectly
// good and we are the problem; suppressing it would throw away a real recipient
// over a sender-side issue that will be fixed.
func Classify(code int, text string) model.Outcome {
	switch {
	case code == 0:
		return model.OutcomeTimeout
	case code >= 200 && code < 300:
		return model.OutcomeSuccess
	case code >= 400 && code < 500:
		return model.OutcomeDeferred
	case code >= 500 && code < 600:
		return classifyPermanent(text)
	default:
		return model.OutcomeDeferred
	}
}

// classifyPermanent decides whose fault a permanent failure is.
//
// Enhanced status codes are consulted first where they are specific. The one
// exception is X.7.1, the catch-all policy code: receivers attach it to spam
// blocks, authentication failures and reputation blocks alike, so where the
// text says something more precise the text wins.
func classifyPermanent(text string) model.Outcome {
	t := strings.ToLower(text)

	if m := enhancedRe.FindStringSubmatch(t); m != nil {
		subject := m[2] + "." + m[3]
		switch {
		case subject == "1.1" || subject == "1.2" || subject == "1.3" || subject == "1.6":
			return model.OutcomeBounce // bad destination address
		case subject == "7.1":
			// The catch-all policy code. Too vague to trust over the wording,
			// so fall through and let the text decide; if nothing matches it
			// lands on blocked at the end anyway.
		case senderFaultSubjects[subject]:
			return model.OutcomeBlocked
		}
	}
	for _, needle := range recipientFaultText {
		if strings.Contains(t, needle) {
			return model.OutcomeBounce
		}
	}
	for _, needle := range senderFaultText {
		if strings.Contains(t, needle) {
			return model.OutcomeBlocked
		}
	}
	// A bare X.7.1 with no informative wording is still a policy refusal.
	if m := enhancedRe.FindStringSubmatch(t); m != nil && m[2]+"."+m[3] == "7.1" {
		return model.OutcomeBlocked
	}
	// Unattributable permanent failure. Treated as a recipient problem because
	// that is the conservative choice for the sending IP's reputation.
	return model.OutcomeBounce
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
