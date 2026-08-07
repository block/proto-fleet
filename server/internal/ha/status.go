package ha

import "time"

type RuntimeRole string
type ObservationStatus string
type EndpointStatus string
type ReasonCode string

const (
	LocalStatusAddress = "127.0.0.1:4000"

	RoleActive       RuntimeRole = "active"
	RolePassive      RuntimeRole = "passive"
	RoleInitializing RuntimeRole = "initializing"
	RoleDegraded     RuntimeRole = "degraded"

	ObservationCurrent     ObservationStatus = "current"
	ObservationStale       ObservationStatus = "stale"
	ObservationUnavailable ObservationStatus = "unavailable"

	EndpointHealthy       EndpointStatus = "healthy"
	EndpointNotApplicable EndpointStatus = "not_applicable"
	EndpointUnhealthy     EndpointStatus = "unhealthy"

	ReasonObservationPending      ReasonCode = "observation_pending"
	ReasonControlPlaneUnavailable ReasonCode = "control_plane_unavailable"
	ReasonObservationStale        ReasonCode = "observation_stale"
	ReasonActivationPending       ReasonCode = "activation_pending"
	ReasonEndpointUnhealthy       ReasonCode = "endpoint_unhealthy"
)

// Status is the intentionally redacted HA operator contract.
type Status struct {
	Version        string            `json:"version"`
	Role           RuntimeRole       `json:"role"`
	Observation    ObservationStatus `json:"observation"`
	ObservedAt     *time.Time        `json:"observed_at,omitempty"`
	LeaseExpiresAt *time.Time        `json:"lease_expires_at,omitempty"`
	Endpoint       EndpointStatus    `json:"endpoint"`
	ReasonCodes    []ReasonCode      `json:"reason_codes"`
}

// Status returns a read-only view composed from ownership, admission, and VIP health.
func (r *Runtime) Status(now time.Time) Status {
	status := Status{
		Role:        RoleInitializing,
		Observation: ObservationUnavailable,
		Endpoint:    EndpointNotApplicable,
		ReasonCodes: []ReasonCode{ReasonObservationPending},
	}
	if r.owner == nil {
		return status
	}
	snapshot := r.owner.Snapshot()
	if snapshot.UpdatedAt.IsZero() {
		return status
	}
	observedAt := snapshot.UpdatedAt.UTC()
	status.ObservedAt = &observedAt
	status.ReasonCodes = nil
	if !snapshot.ObservationAvailable {
		status.Role = RoleDegraded
		status.Observation = ObservationUnavailable
		status.ReasonCodes = []ReasonCode{ReasonControlPlaneUnavailable}
		return status
	}
	if !snapshot.FreshUntil.After(now) {
		status.Role = RoleDegraded
		status.Observation = ObservationStale
		status.ReasonCodes = []ReasonCode{ReasonObservationStale}
		return status
	}
	status.Observation = ObservationCurrent
	if snapshot.State != StateActive {
		status.Role = RolePassive
		return status
	}
	if !r.Active() {
		status.ReasonCodes = []ReasonCode{ReasonActivationPending}
		return status
	}
	if r.config.EndpointHealthy != nil && !r.config.EndpointHealthy() {
		status.Role = RoleDegraded
		status.Endpoint = EndpointUnhealthy
		status.ReasonCodes = []ReasonCode{ReasonEndpointUnhealthy}
		return status
	}
	status.Role = RoleActive
	status.Endpoint = EndpointHealthy
	leaseExpiresAt := snapshot.ExpiresAt.UTC()
	status.LeaseExpiresAt = &leaseExpiresAt
	return status
}

// Passive is the cheap public readiness signal used by the peer health probe.
func (r *Runtime) Passive(now time.Time) bool {
	if r.owner == nil {
		return false
	}
	snapshot := r.owner.Snapshot()
	if snapshot.UpdatedAt.IsZero() || !snapshot.ObservationAvailable || !snapshot.FreshUntil.After(now) || snapshot.State != StatePassive {
		return false
	}
	return r.config.EndpointOwned == nil || !r.config.EndpointOwned()
}
