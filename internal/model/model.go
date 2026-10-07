// Package model holds the core domain types shared across the controller.
package model

import "time"

// Outcome classifies the result of a delivery attempt.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"
	OutcomeDeferred Outcome = "deferred" // 4xx - transient, retry later
	OutcomeBounce   Outcome = "bounce"   // 5xx - permanent, drop
	OutcomeTimeout  Outcome = "timeout"  // network/no response
)

// Message is a single email queued for delivery.
type Message struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customer_id"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Domain      string    `json:"domain"`   // sender domain (reputation dimension)
	Provider    string    `json:"provider"` // destination mailbox provider: gmail/outlook/yahoo
	Subject     string    `json:"subject"`
	Priority    int       `json:"priority"` // higher = sooner
	EnqueuedAt  time.Time `json:"enqueued_at"`
	DeferCount  int       `json:"defer_count"`
	NextAttempt time.Time `json:"next_attempt"`
	AssignedIP  string    `json:"assigned_ip,omitempty"`
}

// Attempt records what happened on a single delivery try.
type Attempt struct {
	MessageID  string    `json:"message_id"`
	Provider   string    `json:"provider"`
	IP         string    `json:"ip"`
	Outcome    Outcome   `json:"outcome"`
	SMTPCode   int       `json:"smtp_code"`
	SMTPText   string    `json:"smtp_text"`
	At         time.Time `json:"at"`
	LatencyMS  int64     `json:"latency_ms"`
	DeferCount int       `json:"defer_count"`
}
