// Package notificationhistory models notifications received from Grafana's
// alertmanager webhook and persisted to the notification_history table.
package notificationhistory

import (
	"context"
	"time"
)

// Notification is one row destined for notification_history.
type Notification struct {
	AlertName      string
	Status         string
	Severity       string
	RuleGroup      string
	Fingerprint    string
	OrganizationID *int64
	DeviceID       string
	Template       string
	Summary        string
	StartsAt       *time.Time
	EndsAt         *time.Time
	Labels         map[string]string
	Annotations    map[string]string
}

// Store persists Notification rows.
type Store interface {
	Insert(ctx context.Context, n *Notification) error
}
