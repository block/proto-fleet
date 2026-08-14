// Package updaterapi defines the narrow local Unix-socket protocol shared by
// Fleet clients and the privileged host updater.
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
	Complete        bool       `json:"complete,omitempty"`
	Phase           Phase      `json:"phase"`
	Message         string     `json:"message,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Error           string     `json:"error,omitempty"`
	RecoveryCommand string     `json:"recovery_command,omitempty"`
	RecoveryPending bool       `json:"recovery_pending,omitempty"`
	LogPath         string     `json:"log_path,omitempty"`
	// Acknowledged records that an operator dismissed this terminal outcome.
	// Clients keep the operation out of their upgrade UI once set.
	Acknowledged bool `json:"acknowledged,omitempty"`
}

type StatusResponse struct {
	Operation *Operation `json:"operation,omitempty"`
}

type TriggerRequest struct {
	OperationID   string `json:"operation_id"`
	TargetVersion string `json:"target_version"`
	Complete      bool   `json:"complete,omitempty"`
}

type TriggerResponse struct {
	Operation Operation `json:"operation"`
}

type AcknowledgeRequest struct {
	OperationID string `json:"operation_id"`
}

type AcknowledgeResponse struct {
	Operation Operation `json:"operation"`
	// AlreadyAcknowledged reports that this call found the dismissal in place
	// rather than recording it. Retries after ambiguous transport results are
	// expected; the distinction lets callers avoid duplicate audit entries.
	AlreadyAcknowledged bool `json:"already_acknowledged,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
