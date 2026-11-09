package devicewrite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/runtimepolicy"
)

const statusComplete = "complete"

// RecoveryEvidence is an operator attestation, not a machine-verified lease.
// Only the offline host operations command calls this boundary, using privileged
// database access while every dispatcher is authoritatively stopped. Evidence
// references must contain no passwords or raw device responses.
type RecoveryEvidence struct {
	AttemptID                 uuid.UUID `json:"attempt_id"`
	PoolRemovalID             uuid.UUID `json:"pool_removal_id,omitempty"`
	OrganizationID            int64     `json:"organization_id"`
	DeviceID                  int64     `json:"device_id"`
	AttemptEpoch              int64     `json:"attempt_epoch"`
	FenceEpoch                int64     `json:"fence_epoch"`
	FenceState                string    `json:"fence_state"`
	OwnerInstanceID           uuid.UUID `json:"owner_instance_id"`
	OperationKind             string    `json:"operation_kind"`
	PriorStatus               string    `json:"prior_status"`
	IdentitySHA256            string    `json:"identity_sha256"`
	Operator                  string    `json:"operator"`
	MaintenanceTermination    string    `json:"maintenance_termination_evidence"`
	OwnerTermination          string    `json:"owner_termination_evidence"`
	TransportQuiescence       string    `json:"transport_quiescence_evidence,omitempty"`
	DeviceQuiescence          string    `json:"device_quiescence_evidence,omitempty"`
	ConfigurationVerification string    `json:"configuration_verification_evidence,omitempty"`
	RestoreHost               bool      `json:"restore_host"`
	QuarantineOrigin          string    `json:"quarantine_origin"`
}

type RecoveryInspection struct {
	Attempt          sqlc.DeviceWriteAttempt `json:"attempt"`
	FenceEpoch       int64                   `json:"fence_epoch"`
	FenceState       string                  `json:"fence_state"`
	IdentitySHA256   string                  `json:"identity_sha256"`
	EvidenceTemplate RecoveryEvidence        `json:"evidence_template"`
	QuarantineOrigin string                  `json:"quarantine_origin"`
}

type RecoveryResult struct {
	Preview    bool      `json:"preview"`
	RecoveryID uuid.UUID `json:"recovery_id"`
	AttemptID  uuid.UUID `json:"attempt_id"`
	FenceEpoch int64     `json:"fence_epoch"`
	FenceState string    `json:"fence_state"`
}

func identityDigest(snapshot json.RawMessage) string {
	sum := sha256.Sum256(snapshot)
	return hex.EncodeToString(sum[:])
}

// InspectRecovery includes unfinished removals whose physical call completed
// but whose verification reservation still needs operator resolution.
func (s *Service) InspectRecovery(ctx context.Context, organizationID, deviceID int64) ([]RecoveryInspection, error) {
	attempts, err := s.queries.ListDeviceWriteRecoveryCandidates(ctx, sqlc.ListDeviceWriteRecoveryCandidatesParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return nil, fmt.Errorf("list recovery attempts: %w", err)
	}
	fence, err := s.queries.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{OrganizationID: organizationID, DeviceID: deviceID})
	if err != nil {
		return nil, lookupError(err)
	}
	out := make([]RecoveryInspection, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, RecoveryInspection{Attempt: a, FenceEpoch: fence.Epoch, FenceState: fence.State, QuarantineOrigin: fence.QuarantineOrigin.String, IdentitySHA256: identityDigest(a.IdentitySnapshot), EvidenceTemplate: RecoveryEvidence{AttemptID: a.AttemptID, OrganizationID: a.OrganizationID, DeviceID: a.DeviceID, AttemptEpoch: a.Epoch, FenceEpoch: fence.Epoch, FenceState: fence.State, QuarantineOrigin: fence.QuarantineOrigin.String, OwnerInstanceID: a.OwnerInstanceID, OperationKind: a.OperationKind, PriorStatus: a.Status, IdentitySHA256: identityDigest(a.IdentitySnapshot)}})
		if a.Status == statusComplete {
			removalID, err := s.queries.GetUnfinishedCompletedPoolRemoval(ctx, sqlc.GetUnfinishedCompletedPoolRemovalParams{OrganizationID: organizationID, DeviceID: deviceID, PhysicalAttemptID: uuid.NullUUID{UUID: a.AttemptID, Valid: true}})
			if err != nil {
				return nil, lookupError(err)
			}
			out[len(out)-1].EvidenceTemplate.PoolRemovalID = removalID
		}
	}
	return out, nil
}

// ResolveOffline binds the exact attempt and current custody under the fence
// lock. It never infers termination from age, a lost connection, or a restart.
// Preview runs the same checks without writing. Applying retains the attempt as
// resolved, records evidence, and invalidates old handles/queued epochs together.
// For an already completed pool clear, it instead fails only the exact unfinished
// removal and keeps both the physical completion record and Fleet assignment.
func (s *Service) ResolveOffline(ctx context.Context, evidence RecoveryEvidence, apply bool) (RecoveryResult, error) {
	var empty RecoveryResult
	required := map[string]string{"operator": evidence.Operator, "maintenance termination": evidence.MaintenanceTermination, "owner termination": evidence.OwnerTermination}
	if evidence.OperationKind == operationMutation {
		required["transport quiescence"] = evidence.TransportQuiescence
		required["device quiescence"] = evidence.DeviceQuiescence
		required["configuration verification"] = evidence.ConfigurationVerification
	}
	if evidence.RestoreHost {
		required["configuration verification"] = evidence.ConfigurationVerification
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" || len(value) > 2048 {
			return empty, fleeterror.NewInvalidArgumentErrorf("recovery requires bounded %s evidence", name)
		}
	}
	if evidence.AttemptID == uuid.Nil || evidence.OwnerInstanceID == uuid.Nil || evidence.OrganizationID <= 0 || evidence.DeviceID <= 0 || evidence.AttemptEpoch < 0 || evidence.FenceEpoch < 0 {
		return empty, fleeterror.NewInvalidArgumentError("recovery requires an exact attempt, owner, device, organization and epochs")
	}
	return db.WithTransactionTimeout(ctx, s.conn, runtimepolicy.CommandTransactionBound, func(q sqlc.Querier) (RecoveryResult, error) {
		fence, err := q.LockDeviceWriteFenceRecord(ctx, sqlc.LockDeviceWriteFenceRecordParams{DeviceID: evidence.DeviceID, OrganizationID: evidence.OrganizationID})
		if err != nil {
			return empty, lookupError(err)
		}
		attempt, err := q.LockDeviceWriteAttemptForRecovery(ctx, evidence.AttemptID)
		if err != nil {
			return empty, lookupError(err)
		}
		if attempt.DeviceID != evidence.DeviceID || attempt.OrganizationID != evidence.OrganizationID || attempt.Epoch != evidence.AttemptEpoch || attempt.OwnerInstanceID != evidence.OwnerInstanceID || attempt.OperationKind != evidence.OperationKind || attempt.Status != evidence.PriorStatus || fence.Epoch != evidence.FenceEpoch || fence.State != evidence.FenceState || fence.QuarantineOrigin.String != evidence.QuarantineOrigin || identityDigest(attempt.IdentitySnapshot) != evidence.IdentitySHA256 {
			return empty, fleeterror.NewFailedPreconditionError("recovery evidence does not match the unresolved attempt and current custody; inspect again")
		}
		switch attempt.Status {
		case statusComplete:
			removalID, err := q.GetUnfinishedCompletedPoolRemoval(ctx, sqlc.GetUnfinishedCompletedPoolRemovalParams{OrganizationID: evidence.OrganizationID, DeviceID: evidence.DeviceID, PhysicalAttemptID: uuid.NullUUID{UUID: attempt.AttemptID, Valid: true}})
			if err != nil {
				return empty, lookupError(err)
			}
			if evidence.PoolRemovalID != removalID {
				return empty, fleeterror.NewFailedPreconditionError("recovery requires the exact unfinished pool removal; inspect again")
			}
		case "running", "unknown":
			if evidence.PoolRemovalID != uuid.Nil {
				return empty, fleeterror.NewFailedPreconditionError("pool-removal recovery evidence requires a completed physical attempt")
			}
		default:
			return empty, blocked()
		}
		if evidence.OperationKind != operationMutation && evidence.OperationKind != operationObservation {
			return empty, blocked()
		}
		if evidence.RestoreHost && fence.State != "reserved" && !(fence.State == "quarantined" && fence.QuarantineOrigin.String == "reserved") {
			return empty, fleeterror.NewFailedPreconditionError("host restoration requires reserved custody or quarantine originating from reservation; active allocations cannot be released by this command")
		}
		result := RecoveryResult{Preview: !apply, AttemptID: attempt.AttemptID, FenceEpoch: fence.Epoch, FenceState: fence.State}
		if !apply {
			return result, nil
		}
		body, err := json.Marshal(evidence)
		if err != nil {
			return empty, fmt.Errorf("encode recovery evidence: %w", err)
		}
		sum := sha256.Sum256(body)
		result.RecoveryID = uuid.New()
		if err := q.RecordDeviceWriteRecovery(ctx, sqlc.RecordDeviceWriteRecoveryParams{RecoveryID: result.RecoveryID, AttemptID: attempt.AttemptID, FenceEpoch: fence.Epoch, PriorStatus: attempt.Status, OperatorName: evidence.Operator, Evidence: body, EvidenceSha256: hex.EncodeToString(sum[:])}); err != nil {
			return empty, fmt.Errorf("record operator recovery: %w", err)
		}
		var changed int64
		if attempt.Status == statusComplete {
			changed, err = q.ResolveCompletedPoolRemoval(ctx, sqlc.ResolveCompletedPoolRemovalParams{RemovalID: evidence.PoolRemovalID, OrganizationID: evidence.OrganizationID, DeviceID: evidence.DeviceID, PhysicalAttemptID: uuid.NullUUID{UUID: attempt.AttemptID, Valid: true}})
		} else {
			changed, err = q.ResolveDeviceWriteAttempt(ctx, attempt.AttemptID)
		}
		if err != nil {
			return empty, fmt.Errorf("resolve device attempt: %w", err)
		}
		if changed != 1 {
			return empty, blocked()
		}
		if err := q.ResolveRecoveredMiningGroupWorkflow(ctx, sqlc.ResolveRecoveredMiningGroupWorkflowParams{
			OrganizationID: evidence.OrganizationID,
			DeviceID:       evidence.DeviceID,
			AttemptID:      uuid.NullUUID{UUID: attempt.AttemptID, Valid: true},
			AttemptEpoch:   attempt.Epoch,
		}); err != nil {
			return empty, fmt.Errorf("resolve recovered mining group workflow: %w", err)
		}
		if err := q.ResolveRecoveredPoolRemoval(ctx, sqlc.ResolveRecoveredPoolRemovalParams{OrganizationID: evidence.OrganizationID, DeviceID: evidence.DeviceID, PhysicalAttemptID: uuid.NullUUID{UUID: attempt.AttemptID, Valid: true}}); err != nil {
			return empty, fmt.Errorf("resolve recovered pool removal: %w", err)
		}
		cutover, err := q.HasPoolConfigurationCutover(ctx, evidence.OrganizationID)
		if err != nil {
			return empty, err
		}
		if !cutover {
			if err := q.DiscardRecoveredLegacyPoolIntents(ctx, sqlc.DiscardRecoveredLegacyPoolIntentsParams{OrganizationID: evidence.OrganizationID, DeviceID: evidence.DeviceID, AttemptID: uuid.NullUUID{UUID: attempt.AttemptID, Valid: true}}); err != nil {
				return empty, err
			}
		}
		advanced, err := q.AdvanceRecoveredDeviceWriteFence(ctx, sqlc.AdvanceRecoveredDeviceWriteFenceParams{DeviceID: evidence.DeviceID, OrganizationID: evidence.OrganizationID, Epoch: fence.Epoch})
		if err != nil {
			return empty, lookupError(err)
		}
		if err := q.CancelPendingDeviceWrites(ctx, evidence.DeviceID); err != nil {
			return empty, fmt.Errorf("cancel recovered stale commands: %w", err)
		}
		result.FenceEpoch = advanced.Epoch
		if evidence.RestoreHost {
			count, err := q.ReleaseDeviceWriteFenceToHost(ctx, sqlc.ReleaseDeviceWriteFenceToHostParams{DeviceID: evidence.DeviceID, OrganizationID: evidence.OrganizationID, Epoch: advanced.Epoch})
			if err != nil {
				return empty, fmt.Errorf("restore verified host custody: %w", err)
			}
			if count != 1 {
				return empty, fleeterror.NewFailedPreconditionError("other unresolved attempts still block host restoration")
			}
			result.FenceState = "host"
		}
		return result, nil
	})
}
