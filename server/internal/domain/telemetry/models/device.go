package models

import "time"

type DeviceID int64

type Device struct {
	ID            DeviceID  `json:"id"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}
