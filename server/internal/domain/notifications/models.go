// Package notifications is the domain layer translating Proto Fleet's channel/silence concepts to Grafana's APIs.
package notifications

import "time"

type ChannelKind string

const (
	ChannelKindWebhook ChannelKind = "webhook"
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

type Channel struct {
	ID              string
	OrganizationID  int64
	Name            string
	Kind            ChannelKind
	Webhook         *WebhookConfig
	Slack           *SlackConfig
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ValidatedAt     *time.Time
	ValidationState ValidationState
	ValidationError string
	HasSecret       bool
}

type RuleTemplate string

const (
	RuleTemplateOffline        RuleTemplate = "offline"
	RuleTemplateHashrate       RuleTemplate = "hashrate"
	RuleTemplateTemperature    RuleTemplate = "temperature"
	RuleTemplatePool           RuleTemplate = "pool"
	RuleTemplateCommandFailure RuleTemplate = "command_failure"
	RuleTemplateTelemetryPoll  RuleTemplate = "telemetry-poll"
)

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

type MaintenanceWindowScopeKind string

const (
	MaintenanceWindowScopeRule   MaintenanceWindowScopeKind = "rule"
	MaintenanceWindowScopeGroup  MaintenanceWindowScopeKind = "group"
	MaintenanceWindowScopeSite   MaintenanceWindowScopeKind = "site"
	MaintenanceWindowScopeDevice MaintenanceWindowScopeKind = "device"
)

type MaintenanceWindowScope struct {
	Kind      MaintenanceWindowScopeKind
	RuleID    string
	GroupID   string
	SiteID    string
	DeviceIDs []string
}

// Active is derived from Now() ∈ [StartsAt, EndsAt) at read time.
type MaintenanceWindow struct {
	ID             string
	OrganizationID int64
	Scope          MaintenanceWindowScope
	StartsAt       time.Time
	EndsAt         time.Time
	Comment        string
	CreatedBy      string
	CreatedAt      time.Time
	Active         bool
}
