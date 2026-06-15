// Package notifications is the domain layer translating Proto Fleet's channel/silence concepts to Grafana's APIs.
package notifications

import "time"

type ChannelKind string

const (
	ChannelKindWebhook ChannelKind = "webhook"
	ChannelKindSMTP    ChannelKind = "smtp"
	ChannelKindSlack   ChannelKind = "slack"
)

// ValidationState is a channel's last test result, tracked by fleet-api since Grafana has no per-receiver field.
type ValidationState string

const (
	ValidationPending ValidationState = "pending"
	ValidationOK      ValidationState = "ok"
	ValidationFailed  ValidationState = "failed"
)

// WebhookConfig is the read shape returned to the UI; BearerHeader is zeroed on reads (see Channel.HasSecret).
type WebhookConfig struct {
	URL          string
	BearerHeader string
}

// SlackConfig configures a Slack incoming-webhook; WebhookURL is the secret and reads return it empty.
type SlackConfig struct {
	WebhookURL string
}

// SMTPConfig is the read shape returned to the UI; Password is write-only and zeroed on reads.
type SMTPConfig struct {
	Host     string
	Port     int32
	Username string
	From     string
	To       []string
	Password string
}

// Channel is a delivery destination; one Channel maps to one Grafana contact point in the caller's org.
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

// RuleTemplate is the closed set of rule kinds from the `template` label; unrecognised values map to empty.
type RuleTemplate string

const (
	RuleTemplateOffline        RuleTemplate = "offline"
	RuleTemplateHashrate       RuleTemplate = "hashrate"
	RuleTemplateTemperature    RuleTemplate = "temperature"
	RuleTemplatePool           RuleTemplate = "pool"
	RuleTemplateCommandFailure RuleTemplate = "command_failure"
	RuleTemplateTelemetryPoll  RuleTemplate = "telemetry-poll"
)

// Rule is a read-only descriptor of one provisioned Grafana alert rule; thresholds are intentionally not editable.
type Rule struct {
	ID              string
	OrganizationID  int64
	Name            string
	Template        RuleTemplate
	Group           string
	Severity        string
	Summary         string
	Description     string
	DurationSeconds int32
	Enabled         bool
}

type SilenceScopeKind string

const (
	SilenceScopeRule   SilenceScopeKind = "rule"
	SilenceScopeGroup  SilenceScopeKind = "group"
	SilenceScopeSite   SilenceScopeKind = "site"
	SilenceScopeDevice SilenceScopeKind = "device"
)

// SilenceScope is the structured form of a Grafana silence matcher set, compiled to/from matchers by fleet-api.
type SilenceScope struct {
	Kind      SilenceScopeKind
	RuleID    string
	GroupID   string
	SiteID    string
	DeviceIDs []string
}

// Silence is a temporary mute; Active is derived from Now() ∈ [StartsAt, EndsAt) at read time.
type Silence struct {
	ID             string
	OrganizationID int64
	Scope          SilenceScope
	StartsAt       time.Time
	EndsAt         time.Time
	Comment        string
	CreatedBy      string
	CreatedAt      time.Time
	Active         bool
}
