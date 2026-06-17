// Package notifications is the domain layer translating Proto Fleet's channel/silence concepts to Grafana's APIs.
package notifications

import "time"

type ChannelKind string

const (
	ChannelKindWebhook ChannelKind = "webhook"
	ChannelKindSMTP    ChannelKind = "smtp"
	ChannelKindSlack   ChannelKind = "slack"
)

type ValidationState string

const (
	ValidationPending ValidationState = "pending"
	ValidationOK      ValidationState = "ok"
	ValidationFailed  ValidationState = "failed"
)

// BearerHeader is zeroed on reads (see Channel.HasSecret).
type WebhookConfig struct {
	URL          string
	BearerHeader string
}

// WebhookURL is the secret and reads return it empty.
type SlackConfig struct {
	WebhookURL string
}

// Password is write-only and zeroed on reads.
type SMTPConfig struct {
	Host     string
	Port     int32
	Username string
	From     string
	To       []string
	Password string
}

type Channel struct {
	ID              string
	OrganizationID  int64
	Name            string
	Kind            ChannelKind
	Webhook         *WebhookConfig
	SMTP            *SMTPConfig
	Slack           *SlackConfig
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ValidatedAt     *time.Time
	ValidationState ValidationState
	ValidationError string
	HasSecret       bool
}
