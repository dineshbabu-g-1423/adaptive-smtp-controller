package smtp

import (
	"testing"

	"github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"
)

// A permanent failure is either about the recipient or about us. Collapsing
// the two is how senders suppress good addresses over their own reputation.
func TestPermanentFailuresAreAttributed(t *testing.T) {
	cases := []struct {
		code int
		text string
		want model.Outcome
		note string
	}{
		{550, "5.1.1 Recipient address rejected: User unknown", model.OutcomeBounce, "dead mailbox"},
		{550, "5.1.1 The email account that you tried to reach does not exist", model.OutcomeBounce, "dead mailbox, gmail wording"},
		{550, "5.1.1 unknown or illegal alias", model.OutcomeBounce, "dead mailbox, icloud wording"},
		{550, "5.7.1 Message rejected as unsolicited", model.OutcomeBlocked, "spam block"},
		{554, "5.7.1 Blocked by reputation", model.OutcomeBlocked, "reputation block"},
		{550, "5.7.606 Access denied, banned sending IP", model.OutcomeBlocked, "banned IP"},
		{550, "5.7.23 SPF validation failed", model.OutcomeBlocked, "authentication, RFC 7372"},
		{550, "5.7.20 No passing DKIM signature found", model.OutcomeBlocked, "authentication, RFC 7372"},
		{552, "5.2.3 Message length exceeds limit", model.OutcomeBounce, "unattributable, conservative default"},
		{421, "4.7.0 Too many messages", model.OutcomeDeferred, "transient stays transient"},
		{250, "2.0.0 OK", model.OutcomeSuccess, "success"},
		{0, "", model.OutcomeTimeout, "no reply"},
	}
	for _, c := range cases {
		if got := Classify(c.code, c.text); got != c.want {
			t.Errorf("%s\n  %d %q\n  got %q, want %q", c.note, c.code, c.text, got, c.want)
		}
	}
}

// The two outcomes agree that retrying is pointless and disagree about whose
// fault it was. That disagreement is the whole reason for the split.
func TestAttributionDrivesSuppression(t *testing.T) {
	dead := Classify(550, "5.1.1 User unknown")
	blocked := Classify(550, "5.7.1 Blocked for policy reasons")

	if dead == blocked {
		t.Fatal("a dead mailbox and a policy block must not classify alike")
	}
	if !dead.Permanent() || !blocked.Permanent() {
		t.Error("both are permanent: neither will succeed on retry")
	}
	if !dead.SuppressRecipient() {
		t.Error("a dead mailbox must suppress the recipient")
	}
	if blocked.SuppressRecipient() {
		t.Error("a policy block says nothing about the recipient and must not suppress it")
	}
}

// X.7.1 is the catch-all policy code and receivers attach it to very different
// situations, so specific wording outranks it.
func TestVaguePolicyCodeDefersToTheText(t *testing.T) {
	if got := Classify(550, "5.7.1 Recipient address rejected: user unknown"); got != model.OutcomeBounce {
		t.Errorf("got %q: a 5.7.1 naming an unknown recipient is still a dead mailbox", got)
	}
}
