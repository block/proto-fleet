// Package notifications is the domain layer that backs the
// settings → notifications UI. It owns the translation between
// Proto Fleet's per-org channel/silence concepts and the
// Grafana sidecar's contact-point / alert-rule / silence APIs.
//
// The package intentionally does not persist anything itself —
// Grafana is the source of truth for rule + receiver state. The
// only Proto Fleet-side persistence is for encrypted secrets
// (webhook bearer headers, SMTP passwords); those live in the
// encrypt service and are looked up by `secret_ref` when fleet-api
// composes the outbound Grafana payload.
//
// Every read filters server-side to the caller's organization id
// (channels via name prefix, rules via labels.organization_id,
// silences via the matcher set). Every write injects the caller's
// organization id into the same fields. The handler layer is the
// authentication boundary; the service refuses zero org ids out of
// an abundance of caution.
//
// IMPORTANT: this package intentionally does NOT expose any
// rule-authoring surface. The set of alert rules is a closed list
// provisioned by ops via the Grafana YAML — operators can only
// list / pause / resume / silence them, never create / edit /
// delete.
package notifications

import "time"

// ChannelKind enumerates the destination types the UI exposes.
type ChannelKind string

const (
	ChannelKindWebhook ChannelKind = "webhook"
	ChannelKindSMTP    ChannelKind = "smtp"
)

// ValidationState mirrors the UI's lightweight state machine for a
// channel's last test result. Grafana doesn't expose a structured
// "last validated" field per receiver, so fleet-api tracks it
// alongside the secret in the encrypt service's metadata bag.
type ValidationState string

const (
	ValidationPending ValidationState = "pending"
	ValidationOK      ValidationState = "ok"
	ValidationFailed  ValidationState = "failed"
)

// WebhookConfig is the read shape returned to the UI. The bearer
// header is zeroed on reads; presence is signalled by Channel.HasSecret.
type WebhookConfig struct {
	URL          string
	BearerHeader string
}

// SMTPConfig is the read shape returned to the UI. Password is
// write-only and zeroed on reads.
type SMTPConfig struct {
	Host     string
	Port     int32
	Username string
	From     string
	To       []string
	Password string
}

// Channel is a destination the rule engine delivers notifications to.
// One Channel == one Grafana contact point in the caller's org.
type Channel struct {
	ID              string
	OrganizationID  int64
	Name            string
	Kind            ChannelKind
	Webhook         *WebhookConfig
	SMTP            *SMTPConfig
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ValidatedAt     *time.Time
	ValidationState ValidationState
	ValidationError string
	HasSecret       bool
}

// RuleTemplate is the closed set of rule "kinds" the provisioned
// YAML exposes. Surfaced on the rule via the `template` label
// (e.g. labels.template="offline"). Anything unrecognised maps to
// the empty string so the UI falls back to the rule title alone.
type RuleTemplate string

const (
	RuleTemplateOffline        RuleTemplate = "offline"
	RuleTemplateHashrate       RuleTemplate = "hashrate"
	RuleTemplateTemperature    RuleTemplate = "temperature"
	RuleTemplatePool           RuleTemplate = "pool"
	RuleTemplateCommandFailure RuleTemplate = "command_failure"
	RuleTemplateTelemetryPoll  RuleTemplate = "telemetry-poll"
)

// Rule is a read-only descriptor of one provisioned Grafana alert
// rule. The UI uses these to render the rules list and decide
// which rule a silence targets. There is no editable scope or
// threshold field here on purpose — operators can pause / resume /
// silence but not alter the SQL behind a rule.
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

// SilenceScopeKind narrows a silence to one slice of the fleet.
type SilenceScopeKind string

const (
	SilenceScopeRule   SilenceScopeKind = "rule"
	SilenceScopeGroup  SilenceScopeKind = "group"
	SilenceScopeSite   SilenceScopeKind = "site"
	SilenceScopeDevice SilenceScopeKind = "device"
)

// SilenceScope is the structured payload behind a Grafana silence
// matcher set. fleet-api compiles this down to the matcher list
// Grafana stores and reverses the mapping on reads.
type SilenceScope struct {
	Kind      SilenceScopeKind
	RuleID    string
	GroupID   string
	SiteID    string
	DeviceIDs []string
}

// Silence is a temporary mute that suppresses a matching rule for a
// finite window. Active is derived from Now() ∈ [StartsAt, EndsAt)
// at read time.
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
