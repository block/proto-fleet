// Package updaterapi defines the narrow local protocol shared by fleetd and
// the privileged host updater. The transport is an HTTP server on a Unix
// socket; this package intentionally contains data types only so neither side
// can accidentally share privileged implementation code.
package updaterapi

import "time"

type Phase string

const (
	PhaseQueued      Phase = "queued"
	PhaseDownloading Phase = "downloading"
	PhaseVerifying   Phase = "verifying"
	PhaseStaging     Phase = "staging"
	PhasePreflight   Phase = "preflight"
	PhaseActivating  Phase = "activating"
	PhaseSucceeded   Phase = "succeeded"
	PhaseFailed      Phase = "failed"
)

func (p Phase) Terminal() bool {
	return p == PhaseSucceeded || p == PhaseFailed
}

type Operation struct {
	ID              string     `json:"id"`
	TargetVersion   string     `json:"target_version"`
	Phase           Phase      `json:"phase"`
	Message         string     `json:"message,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Error           string     `json:"error,omitempty"`
	RecoveryCommand string     `json:"recovery_command,omitempty"`
	LogPath         string     `json:"log_path,omitempty"`
}

type StatusResponse struct {
	Operation *Operation `json:"operation,omitempty"`
}

type TriggerRequest struct {
	OperationID   string `json:"operation_id"`
	TargetVersion string `json:"target_version"`
}

type TriggerResponse struct {
	Operation Operation `json:"operation"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
