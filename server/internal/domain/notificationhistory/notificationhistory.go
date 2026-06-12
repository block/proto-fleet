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

// StoredNotification is a Notification read back from the table,
// carrying the row identity the write path doesn't have plus
// read-time device enrichment (resolved from the device table at
// query time, ” when the device is unknown or deleted).
type StoredNotification struct {
	ID         int64
	ReceivedAt time.Time
	DeviceName string
	DeviceMAC  string
	Notification
}

// Lister reads pages of persisted notifications, newest first. Split
// from Store so write-only consumers (the alertmanager webhook
// receiver and its test fakes) don't have to implement reads.
// beforeID is the keyset cursor: nil for the first page, otherwise
// the previous page's last row id.
type Lister interface {
	List(ctx context.Context, organizationID int64, beforeID *int64, limit int32) ([]StoredNotification, error)
}
