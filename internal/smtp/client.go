package smtp

import "github.com/dineshbabu-g-1423/adaptive-smtp-controller/internal/model"

// Reply is a raw SMTP server response.
type Reply struct {
	Code int
	Text string
}

// Transport is the pluggable interface for actually delivering a message.
// The simulator implements this; a production build would wire a real
// net/smtp dialer behind the same contract.
type Transport interface {
	Send(m model.Message, ip string) Reply
}
