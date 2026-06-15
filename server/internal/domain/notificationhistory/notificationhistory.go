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

// StoredNotification is a Notification read back from the table, with row identity and read-time device enrichment.
type StoredNotification struct {
	ID         int64
	ReceivedAt time.Time
	DeviceName string
	DeviceMAC  string
	Notification
}

// Lister reads pages of persisted notifications, newest first; beforeID is the keyset cursor (nil for the first page).
type Lister interface {
	List(ctx context.Context, organizationID int64, beforeID *int64, limit int32) ([]StoredNotification, error)
}
