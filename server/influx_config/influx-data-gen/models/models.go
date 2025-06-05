package models

import (
	"fmt"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

// Status represents the possible states of a CPU
type Status string

const (
	StatusOK       Status = "OK"
	StatusWarn     Status = "Warn"
	StatusCritical Status = "Critical"
)

// Region represents different datacenter regions
type Region string

const (
	USWest    Region = "us-west"
	USEast    Region = "us-east"
	USCentral Region = "us-central"
	EUWest    Region = "eu-west"
	EUCentral Region = "eu-central"
	APSouth   Region = "ap-south"
)

// Application represents different application types
type Application string

const (
	Webserver    Application = "webserver"
	Database     Application = "database"
	Cache        Application = "cache"
	AuthService  Application = "auth-service"
	APIGateway   Application = "api-gateway"
	MessageQueue Application = "message-queue"
)

// Host represents a server host
type Host string

const (
	Alpha   Host = "Alpha"
	Bravo   Host = "Bravo"
	Charlie Host = "Charlie"
	Delta   Host = "Delta"
	Echo    Host = "Echo"
	Foxtrot Host = "Foxtrot"
)

// CPUMetric represents a single CPU metric measurement
type CPUMetric struct {
	Timestamp    time.Time
	Host         Host
	Region       Region
	Application  Application
	Value        int64
	UsagePercent float64
	Status       Status
}

// AllHosts returns all available hosts
func AllHosts() []Host {
	return []Host{Alpha, Bravo, Charlie, Delta, Echo, Foxtrot}
}

// AllRegions returns all available regions
func AllRegions() []Region {
	return []Region{USWest, USEast, USCentral, EUWest, EUCentral, APSouth}
}

// AllApplications returns all available applications
func AllApplications() []Application {
	return []Application{Webserver, Database, Cache, AuthService, APIGateway, MessageQueue}
}

// AllStatuses returns all available statuses
func AllStatuses() []Status {
	return []Status{StatusOK, StatusWarn, StatusCritical}
}

// ToLineProtocol converts the metric to InfluxDB line protocol format
func (m CPUMetric) ToLineProtocol() string {
	return fmt.Sprintf("cpu,host=%s,region=%s,application=%s val=%di,usage_percent=%.1f,status=\"%s\" %d",
		m.Host,
		m.Region,
		m.Application,
		m.Value,
		m.UsagePercent,
		m.Status,
		m.Timestamp.UnixNano(),
	)
}

func (m CPUMetric) ToPoint() *influxdb3.Point {
	return influxdb3.NewPoint(
		"cpu",
		map[string]string{
			"host":        string(m.Host),
			"region":      string(m.Region),
			"application": string(m.Application),
			"status":      string(m.Status),
		},
		map[string]any{
			"value":         m.Value,
			"usage_percent": m.UsagePercent,
		},
		m.Timestamp,
	)
}
