package models

import (
	"time"
)

type DeviceID string

func (d DeviceID) String() string {
	return string(d)
}

type Device struct {
	ID            DeviceID  `json:"id"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}
