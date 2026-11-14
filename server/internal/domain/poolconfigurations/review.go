package poolconfigurations

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
)

// TargetReviewer must check every target's live authority, ownership, capability,
// and readiness. Binding covers physical identity, worker rendering inputs, mode,
// and write epoch. It must be deterministic and contain no credentials. Called
// during both review and confirmation while the caller's transaction is open.
type TargetReviewer interface {
	ReviewTargets(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64, targets []AssignmentTarget, slots []Endpoint) ([]TargetApproval, error)
}

type TargetApproval struct {
	Target  AssignmentTarget
	Binding string
}

type reviewedTarget struct {
	RevisionID            string `json:",omitempty"`
	VerificationState     string `json:",omitempty"`
	ClearSupported        bool
	DesiredWorkerName     *string `json:",omitempty"`
	PreviousWorkerName    *string `json:",omitempty"`
	DesiredWorkerNameMode string  `json:",omitempty"`
	Change                AssignmentChange
	Binding               string
}

type Review struct {
	WorkerNames     []WorkerNameChange
	ID              string
	Configuration   Configuration
	SettingsChanged bool
	Targets         []AssignmentTarget
	Equipment       []ReviewedEquipment
	ExpiresAt       time.Time
	AssignedCount   int64
}

type ReviewedEquipment struct {
	Kind, Label, DeviceIdentifier string
	ID                            int64
}

func reviewedEquipment(targets []reviewedTarget) []ReviewedEquipment {
	result := make([]ReviewedEquipment, len(targets))
	for i, target := range targets {
		var display equipmentDisplay
		_ = json.Unmarshal([]byte(target.Binding), &display)
		result[i] = ReviewedEquipment{Kind: target.Change.Target.Kind, ID: target.Change.Target.ID, Label: display.Label, DeviceIdentifier: display.Device.DeviceIdentifier}
	}
	return result
}

type ReviewedSave struct {
	Configuration Configuration
	Operation     *Operation
	Replay        bool
	AssignedCount int64
}

// ReviewAssignment captures a concrete selection, including the current
// assignment generation of every target. Dynamic selectors must be expanded by
// the service before this call; confirmation never reevaluates a fleet filter.
func (r *Repository) ReviewAssignment(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64, id string, expectedVersion int64, targets []AssignmentTarget, reviewer TargetReviewer) (Review, error) {
	return r.reviewAssignment(ctx, q, scope, actorID, id, expectedVersion, targets, reviewer, nil, "")
}

func (r *Repository) reviewAssignment(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64, id string, expectedVersion int64, targets []AssignmentTarget, reviewer TargetReviewer, names map[AssignmentTarget]string, mode string) (Review, error) {
	if err := scope.validate(); err != nil {
		return Review{}, err
	}
	if actorID <= 0 || len(targets) == 0 {
		return Review{}, fleeterror.NewInvalidArgumentError("review requires an actor and equipment")
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return Review{}, storageError(err)
	}
	config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
	if err != nil {
		return Review{}, storageError(err)
	}
	if config.Archived || config.Version != expectedVersion {
		return Review{}, staleReview()
	}
	revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id, RevisionID: config.CurrentRevisionID.String})
	if err != nil {
		return Review{}, storageError(err)
	}
	public, err := publicConfiguration(config, revision.Slots)
	if err != nil {
		return Review{}, err
	}
	targets = slices.Clone(targets)
	slices.SortFunc(targets, func(a, b AssignmentTarget) int { return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID)) })
	changes := make([]reviewedTarget, len(targets))
	for i, target := range targets {
		if target.ID <= 0 || target.DeviceID <= 0 || (target.Kind != TargetDevice && target.Kind != TargetInstallation) ||
			(target.Kind == TargetDevice && target.ID != target.DeviceID) || (i > 0 && target.Kind == targets[i-1].Kind && target.ID == targets[i-1].ID) {
			return Review{}, fleeterror.NewInvalidArgumentError("select unique pool assignment targets")
		}
		change := AssignmentChange{Target: target}
		previous, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Review{}, storageError(err)
		}
		if err == nil {
			if !previous.RetiredAt.Valid && previous.AccountID != scope.account() {
				return Review{}, fleeterror.NewForbiddenError("equipment belongs to another configuration owner")
			}
			if previous.DeviceID != target.DeviceID {
				return Review{}, staleAssignment()
			}
			change.ExpectedVersion = previous.Version
		}
		changes[i].Change = change
	}
	bindings, err := reviewTargets(ctx, q, scope, actorID, targets, public.Slots, reviewer)
	if err != nil {
		return Review{}, err
	}
	for i := range changes {
		changes[i].Binding = bindings[i]
		if name, ok := names[changes[i].Change.Target]; ok {
			var binding workerNameBinding
			if err := json.Unmarshal([]byte(bindings[i]), &binding); err != nil {
				return Review{}, fleeterror.NewInternalError("decode reviewed worker binding failed")
			}
			identifier := binding.Device.DeviceIdentifier
			for _, slot := range public.Slots {
				if _, err := RenderWorker(slot.Username, slot.WorkerTemplate, identifier, name); err != nil {
					return Review{}, err
				}
			}
			changes[i].DesiredWorkerName = &name
			changes[i].DesiredWorkerNameMode = mode
			_, before, err := adoptedWorkerNameChange(ctx, q, scope, changes[i].Change.Target, public.RevisionID, name)
			if err != nil {
				return Review{}, err
			}
			changes[i].PreviousWorkerName = before
		}
	}
	if err := checkWorkerNameRetries(ctx, q, scope, public.RevisionID, changes); err != nil {
		return Review{}, err
	}
	encoded, err := json.Marshal(changes)
	if err != nil {
		return Review{}, fleeterror.NewInternalError("encode pool review failed")
	}
	row, err := q.CreatePoolConfigurationReview(ctx, sqlc.CreatePoolConfigurationReviewParams{Kind: "assign", ReviewID: uuid.NewString(), OrganizationID: scope.OrganizationID, ConfigurationID: id, ActorID: actorID, ConfigurationVersion: config.Version, AssignmentVersion: config.AssignmentVersion, Name: config.Name, Slots: revision.Slots, Targets: encoded})
	if err != nil {
		return Review{}, storageError(err)
	}
	summary, err := r.Get(ctx, q, scope, id)
	if err != nil {
		return Review{}, err
	}
	return Review{WorkerNames: workerNameChanges(changes), ID: row.ReviewID, Configuration: public, Targets: targets, Equipment: reviewedEquipment(changes), ExpiresAt: row.ExpiresAt, AssignedCount: summary.AssignedCount}, nil
}

// ReviewEdit prepares an exact, expiring change. The enclosing service must
// authorize configuration access before calling this repository. Credentials are
// encrypted in the review and omitted from its public representation.
func (r *Repository) ReviewEdit(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64, id string, expectedVersion int64, options RevisionOptions, reviewer TargetReviewer) (Review, error) {
	if err := scope.validate(); err != nil {
		return Review{}, err
	}
	if actorID <= 0 {
		return Review{}, fleeterror.NewInvalidArgumentError("review requires an actor")
	}
	options, err := Validate(options)
	if err != nil {
		return Review{}, err
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return Review{}, storageError(err)
	}
	config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
	if err != nil {
		return Review{}, storageError(err)
	}
	if config.Archived || config.Version != expectedVersion {
		return Review{}, staleReview()
	}
	previous, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id, RevisionID: config.CurrentRevisionID.String})
	if err != nil {
		return Review{}, storageError(err)
	}
	oldSlots, err := DecodeSlots(previous.Slots)
	if err != nil {
		return Review{}, err
	}
	options.Slots = slices.Clone(options.Slots)
	for i := range options.Slots {
		if options.Slots[i].ID != "" && !slices.ContainsFunc(oldSlots, func(slot StoredSlot) bool { return slot.ID == options.Slots[i].ID }) {
			return Review{}, fleeterror.NewInvalidArgumentError("pool slot identity must belong to this configuration; leave empty for a new slot")
		}
		if options.Slots[i].ID == "" {
			options.Slots[i].ID = uuid.NewString()
		}
	}
	encoded, err := EncryptSlots(r.secrets, options.Slots, oldSlots)
	if err != nil {
		return Review{}, err
	}
	proposed, err := DecodeSlots(encoded)
	if err != nil {
		return Review{}, err
	}
	equal, err := r.Equivalent(oldSlots, proposed)
	if err != nil {
		return Review{}, err
	}
	// Keep original slot identities for no-op settings, just as Revise does.
	if equal {
		encoded = previous.Slots
	}
	public, err := publicConfiguration(config, encoded)
	if err != nil {
		return Review{}, err
	}
	public.Name = options.Name
	result := Review{Configuration: public, SettingsChanged: !equal, Targets: []AssignmentTarget{}}
	changes := []reviewedTarget{}
	if !equal {
		assignments, err := q.ListPoolConfigurationAssignments(ctx, sqlc.ListPoolConfigurationAssignmentsParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: id})
		if err != nil {
			return Review{}, storageError(err)
		}
		for _, assignment := range assignments {
			target := AssignmentTarget{Kind: assignment.TargetKind, ID: assignment.TargetID, DeviceID: assignment.DeviceID}
			result.Targets = append(result.Targets, target)
			changes = append(changes, reviewedTarget{Change: AssignmentChange{Target: target, ExpectedVersion: assignment.Version}})
		}
	}
	bindings, err := reviewTargets(ctx, q, scope, actorID, result.Targets, public.Slots, reviewer)
	if err != nil {
		return Review{}, err
	}
	for i := range changes {
		changes[i].Binding = bindings[i]
	}
	targets, err := json.Marshal(changes)
	if err != nil {
		return Review{}, fleeterror.NewInternalError("encode pool review failed")
	}
	row, err := q.CreatePoolConfigurationReview(ctx, sqlc.CreatePoolConfigurationReviewParams{Kind: "edit", ReviewID: uuid.NewString(), OrganizationID: scope.OrganizationID, ConfigurationID: id, ActorID: actorID, ConfigurationVersion: config.Version, AssignmentVersion: config.AssignmentVersion, Name: options.Name, Slots: encoded, SettingsChanged: !equal, Targets: targets})
	if err != nil {
		return Review{}, storageError(err)
	}
	result.ID, result.ExpiresAt = row.ReviewID, row.ExpiresAt
	result.Equipment = reviewedEquipment(changes)
	return result, nil
}

// ConfirmReview revalidates the full reviewed set before changing any configuration
// or assignment. All changes and durable work commit atomically in the caller's
// transaction. A repeated confirmation returns the original accepted result.
// The caller supplies credential authorization for live, unaccepted reviews;
// it runs under the review lock before any mutation and is skipped for replay.
func (r *Repository) ConfirmReview(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64, reviewID string, reviewer TargetReviewer, authorize CredentialProof) (ReviewedSave, error) {
	if err := scope.validate(); err != nil {
		return ReviewedSave{}, err
	}
	if _, err := q.LockPoolConfigurationImportOrganization(ctx, scope.OrganizationID); err != nil {
		return ReviewedSave{}, storageError(err)
	}
	review, err := q.LockPoolConfigurationReview(ctx, sqlc.LockPoolConfigurationReviewParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ActorID: actorID, ReviewID: reviewID})
	if err != nil {
		return ReviewedSave{}, storageError(err)
	}
	if review.SavedVersion.Valid {
		current, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: review.ConfigurationID})
		if err != nil {
			return ReviewedSave{}, storageError(err)
		}
		config := sqlc.PoolConfiguration{ConfigurationID: review.ConfigurationID, Name: review.Name, CurrentRevisionID: review.SavedRevisionID, Version: review.SavedVersion.Int64, IsDefault: current.IsDefault}
		public, err := publicConfiguration(config, review.Slots)
		if err != nil {
			return ReviewedSave{}, err
		}
		result := ReviewedSave{Configuration: public, Replay: true}
		if review.OperationID.Valid {
			operation, err := r.GetOperation(ctx, q, scope, review.OperationID.String)
			if err != nil {
				return ReviewedSave{}, err
			}
			result.Operation = &operation
		}
		return result, nil
	}
	if !review.Valid {
		return ReviewedSave{}, expiredUnacceptedReview()
	}
	if authorize != nil {
		if err := authorize(ctx, q); err != nil {
			return ReviewedSave{}, err
		}
	}
	config, err := q.LockPoolConfiguration(ctx, sqlc.LockPoolConfigurationParams{OrganizationID: scope.OrganizationID, AccountID: scope.account(), ConfigurationID: review.ConfigurationID})
	if err != nil {
		return ReviewedSave{}, storageError(err)
	}
	if config.Archived || config.Version != review.ConfigurationVersion || (review.Kind == "edit" && config.AssignmentVersion != review.AssignmentVersion) {
		return ReviewedSave{}, staleReview()
	}
	var reviewed []reviewedTarget
	if err := json.Unmarshal(review.Targets, &reviewed); err != nil {
		return ReviewedSave{}, fleeterror.NewInternalError("decode pool review failed")
	}
	public, err := publicConfiguration(config, review.Slots)
	if err != nil {
		return ReviewedSave{}, err
	}
	targets := make([]AssignmentTarget, len(reviewed))
	changes := make([]AssignmentChange, len(reviewed))
	for i, item := range reviewed {
		targets[i], changes[i] = item.Change.Target, item.Change
		changes[i].ReviewedBinding = item.Binding
	}
	bindings, err := reviewTargets(ctx, q, scope, actorID, targets, public.Slots, reviewer)
	if err != nil {
		return ReviewedSave{}, err
	}
	for i, item := range reviewed {
		if item.Binding != bindings[i] {
			return ReviewedSave{}, staleReview()
		}
	}
	if err := checkWorkerNameRetries(ctx, q, scope, public.RevisionID, reviewed); err != nil {
		return ReviewedSave{}, err
	}
	// Recheck the original binding before changing metadata. The equipment
	// reviewer holds physical identity/write admission through this transaction.
	workerChanges := workerNameChanges(reviewed)
	if len(workerChanges) > 0 {
		if err := authorizeWorkerNames(ctx, q, scope, actorID); err != nil {
			return ReviewedSave{}, err
		}
		for i, item := range reviewed {
			if item.DesiredWorkerName == nil || item.Change.Target.Kind != TargetInstallation {
				continue
			}
			n, err := q.ClearInheritedPoolWorkerName(ctx, sqlc.ClearInheritedPoolWorkerNameParams{OrganizationID: scope.OrganizationID, TargetKind: item.Change.Target.Kind, TargetID: item.Change.Target.ID, Version: item.Change.ExpectedVersion})
			if err != nil {
				return ReviewedSave{}, storageError(err)
			}
			changes[i].ExpectedVersion += n
		}
		for _, change := range workerChanges {
			var n int64
			var err error
			if change.HashboardID != 0 {
				n, err = q.UpdateHashboardWorkerName(ctx, sqlc.UpdateHashboardWorkerNameParams{OrgID: scope.OrganizationID, HashboardID: change.HashboardID, WorkerName: change.After, WorkerNameMode: change.Mode})
			} else {
				n, err = q.UpdateDeviceWorkerName(ctx, sqlc.UpdateDeviceWorkerNameParams{DeviceIdentifier: change.DeviceIdentifier, WorkerName: sql.NullString{String: change.After, Valid: true}})
			}
			if err := targetMutationResult(n, err); err != nil {
				return ReviewedSave{}, err
			}
		}
		updated, err := reviewTargets(ctx, q, scope, actorID, targets, public.Slots, reviewer)
		if err != nil {
			return ReviewedSave{}, err
		}
		for i := range changes {
			changes[i].ReviewedBinding = updated[i]
		}
	}
	saved := SaveResult{Configuration: public}
	if review.Kind == "edit" {
		stored, err := DecodeSlots(review.Slots)
		if err != nil {
			return ReviewedSave{}, err
		}
		options := RevisionOptions{Name: review.Name, Slots: make([]SlotOptions, len(stored))}
		for i, slot := range stored {
			plain, err := r.secrets.Decrypt(slot.PasswordCiphertext)
			if err != nil {
				return ReviewedSave{}, fleeterror.NewInternalError("read reviewed pool credential failed")
			}
			password := string(plain)
			options.Slots[i] = SlotOptions{ID: slot.ID, URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, Password: &password}
		}
		saved, err = r.Revise(ctx, q, scope, config.ConfigurationID, config.Version, options)
		if err != nil {
			return ReviewedSave{}, err
		}
		if saved.SettingsChanged != review.SettingsChanged {
			return ReviewedSave{}, staleReview()
		}
	}
	result := ReviewedSave{Configuration: saved.Configuration}
	var operationID sql.NullString
	if (review.Kind == "assign" || saved.SettingsChanged) && len(changes) > 0 {
		operation, err := r.Assign(ctx, q, scope, actorID, config.ConfigurationID, saved.Configuration.RevisionID, changes)
		if err != nil {
			return ReviewedSave{}, err
		}
		operationID = sql.NullString{String: operation.ID, Valid: true}
		result.Operation = &operation
	}
	n, err := q.SavePoolConfigurationReviewResult(ctx, sqlc.SavePoolConfigurationReviewResultParams{OrganizationID: scope.OrganizationID, ReviewID: reviewID, SavedVersion: sql.NullInt64{Int64: saved.Configuration.Version, Valid: true}, SavedRevisionID: sql.NullString{String: saved.Configuration.RevisionID, Valid: true}, OperationID: operationID})
	if err := targetMutationResult(n, err); err != nil {
		return ReviewedSave{}, err
	}
	return result, nil
}

func reviewTargets(ctx context.Context, q sqlc.Querier, scope Scope, actorID int64, targets []AssignmentTarget, slots []Endpoint, reviewer TargetReviewer) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	if reviewer == nil {
		return nil, fleeterror.NewInternalError("pool equipment review is unavailable")
	}
	approvals, err := reviewer.ReviewTargets(ctx, q, scope, actorID, slices.Clone(targets), slices.Clone(slots))
	if err != nil {
		return nil, err
	}
	if len(approvals) != len(targets) {
		return nil, fleeterror.NewInternalError("pool equipment review omitted targets")
	}
	byTarget := make(map[AssignmentTarget]string, len(approvals))
	for _, approval := range approvals {
		if approval.Binding == "" || byTarget[approval.Target] != "" {
			return nil, fleeterror.NewInternalError("invalid pool equipment review")
		}
		byTarget[approval.Target] = approval.Binding
	}
	bindings := make([]string, len(targets))
	for i, target := range targets {
		bindings[i] = byTarget[target]
		if bindings[i] == "" {
			return nil, fleeterror.NewInternalError("pool equipment review omitted targets")
		}
	}
	return bindings, nil
}

// Only the locked, expired review with no saved result proves non-acceptance.
func expiredUnacceptedReview() error {
	return fleeterror.NewErrorWithEndpointCode("This review expired without being accepted. Review your changes again.", connect.CodeFailedPrecondition, int32(pb.ConfirmReviewErrorCode_CONFIRM_REVIEW_ERROR_CODE_EXPIRED_UNACCEPTED))
}

func staleReview() error {
	return fleeterror.NewFailedPreconditionError("configuration or equipment changed; refresh and review again")
}

// Only display identity is exposed from the private, frozen dispatch binding.
type equipmentDisplay struct {
	Label  string
	Device struct {
		ID               int64
		DeviceIdentifier string
	}
}
