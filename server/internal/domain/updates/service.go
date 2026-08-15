package updates

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"golang.org/x/mod/semver"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/admissionctx"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

// Channel is an org's release channel in its persisted (DB string) form; the
// values match the release_channel_setting CHECK constraint.
type Channel string

const (
	// ChannelStable offers only stable releases. It is also the read-side
	// default for orgs that never chose a channel.
	ChannelStable Channel = "stable"
	// ChannelStableAndRC also offers release candidates.
	ChannelStableAndRC Channel = "stable_and_rc"

	executorStatusTimeout   = 2 * time.Second
	executorMutationTimeout = 6 * time.Second
	upgradeAuditTimeout     = 5 * time.Second

	upgradeTriggeredEventType    = "instance_upgrade_triggered"
	upgradeAcknowledgedEventType = "instance_upgrade_acknowledged"
)

// UpdateStatus is the gated, channel-filtered update offer for one org.
// LatestEligible is nil — and InstallCommand empty — unless UpdateAvailable.
type UpdateStatus struct {
	CurrentVersion    string
	Channel           Channel
	LatestEligible    *Release
	StatusAvailable   bool
	UpdateAvailable   bool
	InstallCommand    string
	OneClickAvailable bool
}

type UpgradeStatus struct {
	ExecutorAvailable bool
	Operation         *updaterapi.Operation
}

// snapshotProvider is the narrow slice of the Checker the service reads;
// tests substitute a fake.
type snapshotProvider interface {
	Snapshot() Snapshot
}

// channelSettingQuerier is the slice of the generated sqlc querier the
// service uses for per-org channel settings.
type channelSettingQuerier interface {
	GetReleaseChannelSetting(ctx context.Context, organizationID int64) (sqlc.ReleaseChannelSetting, error)
	UpsertReleaseChannelSetting(ctx context.Context, arg sqlc.UpsertReleaseChannelSettingParams) (sqlc.ReleaseChannelSetting, error)
}

// Service composes the checker snapshot, the per-org channel setting, and the
// running server version into update status answers. Channel eligibility is
// applied at read time, so a channel flip takes effect on the next call
// without waiting for a fresh GitHub fetch.
type Service struct {
	cfg            Config
	currentVersion string
	snapshots      snapshotProvider
	queries        channelSettingQuerier
	logger         *slog.Logger
	executor       executorClient
	activitySvc    *activity.Service
}

// NewService creates the updates domain service. serverVersion is the running
// fleetd build (the ldflags main.version) — never a client bundle version.
func NewService(
	cfg Config,
	serverVersion string,
	snapshots snapshotProvider,
	queries channelSettingQuerier,
	activityServices ...*activity.Service,
) *Service {
	var activitySvc *activity.Service
	if len(activityServices) > 0 {
		activitySvc = activityServices[0]
	}
	service := newService(cfg, serverVersion, snapshots, queries, slog.Default())
	service.activitySvc = activitySvc
	return service
}

func newService(cfg Config, serverVersion string, snapshots snapshotProvider, queries channelSettingQuerier, logger *slog.Logger) *Service {
	return &Service{
		cfg:            cfg,
		currentVersion: serverVersion,
		snapshots:      snapshots,
		queries:        queries,
		logger:         logger,
		executor:       newExecutorClient(cfg.UpdaterSocketPath),
	}
}

// GetUpdateStatus computes the org's channel-gated update offer from the
// latest checker snapshot. A non-semver running version ("dev", nightlies)
// never reports an update: unparseable must read as "nothing to offer", not
// "show latest", and it is not worth more than a Debug line.
func (s *Service) GetUpdateStatus(ctx context.Context, organizationID int64) (UpdateStatus, error) {
	channel, err := s.releaseChannel(ctx, organizationID)
	if err != nil {
		return UpdateStatus{}, err
	}
	return s.updateStatusForChannel(channel, s.executorAvailable(ctx)), nil
}

func (s *Service) updateStatusForChannel(channel Channel, oneClickAvailable bool) UpdateStatus {
	snapshot := s.snapshots.Snapshot()
	status := UpdateStatus{
		CurrentVersion:    s.currentVersion,
		Channel:           channel,
		OneClickAvailable: oneClickAvailable,
	}
	if !semver.IsValid(s.currentVersion) {
		s.logger.Debug("update check skipped: running version is not semver", "version", s.currentVersion)
		return status
	}
	status.StatusAvailable = channelStatusAvailable(channel, snapshot, s.currentVersion)
	if !status.StatusAvailable {
		return status
	}

	candidate := eligibleCandidate(channel, snapshot)
	if candidate == nil || semver.Compare(candidate.Version, s.currentVersion) <= 0 {
		return status
	}
	command, ok := installCommand(s.cfg.DownloadBaseURL, candidate.Version)
	if !ok {
		// Defensive: a candidate the command guard rejects is never offered
		// at all — an offer without a runnable command would be a dead end.
		return status
	}

	eligible := *candidate
	status.LatestEligible = &eligible
	status.UpdateAvailable = true
	status.InstallCommand = command
	return status
}

func (s *Service) executorAvailable(ctx context.Context) bool {
	if s.executor == nil {
		return false
	}
	statusCtx, cancel := context.WithTimeout(ctx, executorStatusTimeout)
	defer cancel()
	_, err := s.executor.Status(statusCtx)
	return err == nil
}

// TriggerUpgrade re-derives the eligible offer at mutation time. The browser
// cannot ask the privileged host executor to run a URL, command, downgrade,
// or stale release that is no longer offered by the selected channel.
func (s *Service) TriggerUpgrade(ctx context.Context, organizationID int64, operationID, targetVersion string) (updaterapi.Operation, error) {
	if err := validateOperationID(operationID); err != nil {
		return updaterapi.Operation{}, err
	}

	// Consult the updater's durable operation before re-deriving a fresh offer.
	// An exact retry must remain recoverable after the upgrade changes the
	// running version or the selected release channel changes. The operation's
	// complete-mode bit is part of its identity so the Fleet UI cannot claim an
	// HA completion operation that happens to reuse its ID and target.
	if s.executor != nil {
		operation, replay, err := s.currentUpgradeReplay(ctx, operationID, targetVersion)
		if err != nil {
			return updaterapi.Operation{}, err
		}
		if replay {
			if err := ctx.Err(); err != nil {
				return updaterapi.Operation{}, fmt.Errorf("trigger canceled before host mutation: %w", err)
			}
			operationCtx, cancelOperation, ok := admissionctx.DetachRequestCancellation(ctx)
			if !ok {
				return updaterapi.Operation{}, fleeterror.NewInternalError("upgrade request is missing active-runtime admission")
			}
			defer cancelOperation()
			if err := operationCtx.Err(); err != nil {
				return updaterapi.Operation{}, fmt.Errorf("trigger canceled before host mutation: %w", err)
			}
			s.logUpgradeTriggered(operationCtx, organizationID, operation)
			return operation, nil
		}
	}

	channel, err := s.releaseChannel(ctx, organizationID)
	if err != nil {
		return updaterapi.Operation{}, err
	}
	// Executor reachability was already probed above for replay. Only the
	// channel-filtered offer matters for admission of a genuinely new ID.
	status := s.updateStatusForChannel(channel, false)
	if !status.UpdateAvailable || status.LatestEligible == nil {
		return updaterapi.Operation{}, fleeterror.NewFailedPreconditionError("no eligible update is available")
	}
	if targetVersion != status.LatestEligible.Version {
		return updaterapi.Operation{}, fleeterror.NewFailedPreconditionErrorf(
			"target %q is no longer the eligible update", targetVersion)
	}
	if s.executor == nil {
		return updaterapi.Operation{}, fleeterror.NewFailedPreconditionError("one-click updates are not installed on this host")
	}
	if err := ctx.Err(); err != nil {
		return updaterapi.Operation{}, fmt.Errorf("trigger canceled before host mutation: %w", err)
	}

	// Authorization and target validation are complete. Detach the privileged
	// local mutation from browser cancellation while preserving the exact
	// active-runtime lifetime that admitted this request. A missing lifetime
	// means the active gate was bypassed, so no host mutation is allowed.
	operationCtx, cancelOperation, ok := admissionctx.DetachRequestCancellation(ctx)
	if !ok {
		return updaterapi.Operation{}, fleeterror.NewInternalError("upgrade request is missing active-runtime admission")
	}
	defer cancelOperation()
	if err := operationCtx.Err(); err != nil {
		return updaterapi.Operation{}, fmt.Errorf("trigger canceled before host mutation: %w", err)
	}

	operation, err := s.triggerUpgrade(operationCtx, operationID, targetVersion)
	if err == nil {
		s.logUpgradeTriggered(operationCtx, organizationID, operation)
		return operation, nil
	}
	if cancelErr := operationCtx.Err(); cancelErr != nil {
		return updaterapi.Operation{}, fmt.Errorf("trigger canceled during host mutation: %w", cancelErr)
	}

	// A reset, timeout, or malformed accepted response does not prove that the
	// mutation failed. Retry once with the same caller-supplied operation ID so
	// a request still finishing admission can return its durable operation, or
	// a request that never arrived can be safely admitted. If that response is
	// also inconclusive, an exact status match is authoritative and cannot be
	// confused with another operator's same-target request.
	if ambiguousExecutorResult(err) {
		operation, retryErr := s.triggerUpgrade(operationCtx, operationID, targetVersion)
		if retryErr == nil {
			s.logUpgradeTriggered(operationCtx, organizationID, operation)
			return operation, nil
		}
		if cancelErr := operationCtx.Err(); cancelErr != nil {
			return updaterapi.Operation{}, fmt.Errorf("trigger canceled during host mutation retry: %w", cancelErr)
		}
		if reconciled, ok := s.reconcileUpgrade(operationCtx, operationID, targetVersion); ok {
			s.logUpgradeTriggered(operationCtx, organizationID, reconciled)
			return reconciled, nil
		}
		if cancelErr := operationCtx.Err(); cancelErr != nil {
			return updaterapi.Operation{}, fmt.Errorf("trigger canceled during host mutation reconciliation: %w", cancelErr)
		}
	}
	return updaterapi.Operation{}, mapExecutorTriggerError(err)
}

func (s *Service) currentUpgradeReplay(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, bool, error) {
	statusCtx, cancel := context.WithTimeout(ctx, executorStatusTimeout)
	defer cancel()
	status, err := s.executor.Status(statusCtx)
	if err != nil || status.Operation == nil || status.Operation.ID != operationID {
		return updaterapi.Operation{}, false, nil
	}
	operation := *status.Operation
	if operation.TargetVersion != targetVersion || operation.Complete {
		return updaterapi.Operation{}, false, fleeterror.NewFailedPreconditionError(
			"operation id is already associated with another update",
		)
	}
	return operation, true, nil
}

func (s *Service) triggerUpgrade(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, error) {
	triggerCtx, cancel := context.WithTimeout(ctx, executorMutationTimeout)
	defer cancel()
	return s.executor.Trigger(triggerCtx, operationID, targetVersion)
}

func ambiguousExecutorResult(err error) bool {
	var transportErr *updaterapi.TransportError
	var protocolErr *updaterapi.ProtocolError
	return errors.As(err, &transportErr) || errors.As(err, &protocolErr)
}

func (s *Service) reconcileUpgrade(ctx context.Context, operationID, targetVersion string) (updaterapi.Operation, bool) {
	statusCtx, cancel := context.WithTimeout(ctx, executorStatusTimeout)
	defer cancel()
	status, err := s.executor.Status(statusCtx)
	if err != nil || status.Operation == nil {
		return updaterapi.Operation{}, false
	}
	operation := *status.Operation
	if operation.ID != operationID || operation.TargetVersion != targetVersion || operation.Complete {
		return updaterapi.Operation{}, false
	}
	return operation, true
}

// executorCallMessages carries the per-verb operator guidance for the
// transport failures every executor call can hit. mapExecutorCallError owns
// which failure classes are retryable Unavailable results, so a new transport
// failure class is classified in one place.
type executorCallMessages struct {
	unavailable string
	unconfirmed string
	canceled    string
}

func mapExecutorCallError(err error, wrap string, messages executorCallMessages, mapHTTPError func(*updaterapi.HTTPError) error) error {
	if errors.Is(err, updaterapi.ErrUnavailable) {
		return fleeterror.NewUnavailableErrorf(messages.unavailable)
	}
	var httpErr *updaterapi.HTTPError
	if errors.As(err, &httpErr) {
		return mapHTTPError(httpErr)
	}
	if ambiguousExecutorResult(err) || errors.Is(err, context.DeadlineExceeded) {
		return fleeterror.NewUnavailableErrorf(messages.unconfirmed)
	}
	if errors.Is(err, context.Canceled) {
		return fleeterror.NewUnavailableErrorf(messages.canceled)
	}
	return fmt.Errorf("%s: %w", wrap, err)
}

func mapExecutorTriggerError(err error) error {
	return mapExecutorCallError(err, "trigger host upgrade", executorCallMessages{
		unavailable: "host updater is unavailable; use the install command instead",
		unconfirmed: "host updater did not confirm the upgrade; check its status before retrying",
		canceled:    "host updater canceled the upgrade request before confirming it",
	}, func(httpErr *updaterapi.HTTPError) error {
		switch httpErr.StatusCode {
		case http.StatusConflict:
			return fleeterror.NewAlreadyExistsError(httpErr.Message)
		case http.StatusBadRequest, http.StatusPreconditionFailed:
			return fleeterror.NewFailedPreconditionError(httpErr.Message)
		case http.StatusServiceUnavailable:
			return fleeterror.NewUnavailableErrorf("host updater is temporarily unavailable; use the install command instead")
		}
		return fmt.Errorf("host updater rejected trigger with HTTP status %d", httpErr.StatusCode)
	})
}

func (s *Service) logUpgradeTriggered(ctx context.Context, organizationID int64, operation updaterapi.Operation) {
	s.logUpgradeEvent(ctx, organizationID, upgradeTriggeredEventType,
		fmt.Sprintf("Triggered instance upgrade to %s", operation.TargetVersion),
		map[string]any{
			"operation_id":   operation.ID,
			"target_version": operation.TargetVersion,
		}, upgradeOperationIdempotencyKey(upgradeTriggeredEventType, organizationID, operation))
}

func (s *Service) logUpgradeEvent(
	ctx context.Context,
	organizationID int64,
	eventType, description string,
	metadata map[string]any,
	idempotencyKey string,
) {
	event := activitymodels.Event{
		Category:       activitymodels.CategorySystem,
		Type:           eventType,
		OrganizationID: &organizationID,
		Description:    description,
		Metadata:       metadata,
		IdempotencyKey: idempotencyKey,
	}
	activity.StampActor(ctx, &event)
	auditCtx, cancel := context.WithTimeout(ctx, upgradeAuditTimeout)
	defer cancel()
	s.activitySvc.Log(auditCtx, event)
}

// AcknowledgeUpgrade durably dismisses a terminal upgrade outcome on the host
// updater, so the operation stops resurfacing in every session. The mutation
// and audit are independently idempotent; ambiguous updater responses are
// retried and reconciled before the caller is asked to retry.
func (s *Service) AcknowledgeUpgrade(
	ctx context.Context,
	organizationID int64,
	operationID string,
	expectedStartedAt time.Time,
	expectedOutcomeRevision uint64,
) (updaterapi.Operation, error) {
	if err := validateOperationID(operationID); err != nil {
		return updaterapi.Operation{}, err
	}
	if expectedStartedAt.IsZero() {
		return updaterapi.Operation{}, fleeterror.NewInvalidArgumentError("expected started at is required")
	}
	if expectedOutcomeRevision == 0 {
		return updaterapi.Operation{}, fleeterror.NewInvalidArgumentError("expected outcome revision is required")
	}
	if s.executor == nil {
		return updaterapi.Operation{}, fleeterror.NewFailedPreconditionError("one-click updates are not installed on this host")
	}
	if err := ctx.Err(); err != nil {
		return updaterapi.Operation{}, fmt.Errorf("acknowledge canceled before host mutation: %w", err)
	}

	// A browser reload or closed tab must not abort an admitted host mutation
	// mid-way. Detach caller cancellation while preserving the active-runtime
	// lifetime that admitted the request, exactly as TriggerUpgrade does.
	operationCtx, cancelOperation, ok := admissionctx.DetachRequestCancellation(ctx)
	if !ok {
		return updaterapi.Operation{}, fleeterror.NewInternalError("acknowledge request is missing active-runtime admission")
	}
	defer cancelOperation()
	if err := operationCtx.Err(); err != nil {
		return updaterapi.Operation{}, fmt.Errorf("acknowledge canceled before host mutation: %w", err)
	}

	expectedStartedAt = expectedStartedAt.UTC()
	operation, err := s.acknowledgeUpgrade(operationCtx, operationID, expectedStartedAt, expectedOutcomeRevision)
	if err == nil {
		s.logUpgradeAcknowledged(operationCtx, organizationID, operation)
		return operation, nil
	}
	if cancelErr := operationCtx.Err(); cancelErr != nil {
		return updaterapi.Operation{}, fmt.Errorf("acknowledge canceled during host mutation: %w", cancelErr)
	}

	// A lost or malformed success response does not prove that the dismissal
	// failed. Retry the idempotent host mutation once, then reconcile against
	// exact durable status. Any confirmed acknowledgement attempts the same
	// database-idempotent audit, so both an ambiguous first response and an
	// unrelated repeat converge on one activity row.
	if ambiguousExecutorResult(err) {
		operation, retryErr := s.acknowledgeUpgrade(operationCtx, operationID, expectedStartedAt, expectedOutcomeRevision)
		if retryErr == nil {
			s.logUpgradeAcknowledged(operationCtx, organizationID, operation)
			return operation, nil
		}
		if cancelErr := operationCtx.Err(); cancelErr != nil {
			return updaterapi.Operation{}, fmt.Errorf("acknowledge canceled during host mutation retry: %w", cancelErr)
		}
		if reconciled, ok := s.reconcileAcknowledgement(operationCtx, operationID, expectedStartedAt, expectedOutcomeRevision); ok {
			s.logUpgradeAcknowledged(operationCtx, organizationID, reconciled)
			return reconciled, nil
		}
		if cancelErr := operationCtx.Err(); cancelErr != nil {
			return updaterapi.Operation{}, fmt.Errorf("acknowledge canceled during host mutation reconciliation: %w", cancelErr)
		}
	}
	return updaterapi.Operation{}, mapExecutorAcknowledgeError(err)
}

func (s *Service) acknowledgeUpgrade(
	ctx context.Context,
	operationID string,
	expectedStartedAt time.Time,
	expectedOutcomeRevision uint64,
) (updaterapi.Operation, error) {
	acknowledgeCtx, cancel := context.WithTimeout(ctx, executorMutationTimeout)
	defer cancel()
	operation, _, err := s.executor.Acknowledge(acknowledgeCtx, operationID, expectedStartedAt, expectedOutcomeRevision)
	if err != nil {
		return updaterapi.Operation{}, err
	}
	if operation.ID != operationID ||
		!operation.StartedAt.Equal(expectedStartedAt) ||
		operation.OutcomeRevision != expectedOutcomeRevision ||
		!operation.Acknowledged {
		return updaterapi.Operation{}, &updaterapi.ProtocolError{Cause: errors.New("host updater response did not confirm acknowledgement")}
	}
	return operation, nil
}

func (s *Service) reconcileAcknowledgement(
	ctx context.Context,
	operationID string,
	expectedStartedAt time.Time,
	expectedOutcomeRevision uint64,
) (updaterapi.Operation, bool) {
	statusCtx, cancel := context.WithTimeout(ctx, executorStatusTimeout)
	defer cancel()
	status, err := s.executor.Status(statusCtx)
	if err != nil || status.Operation == nil {
		return updaterapi.Operation{}, false
	}
	operation := *status.Operation
	if operation.ID != operationID ||
		!operation.StartedAt.Equal(expectedStartedAt) ||
		operation.OutcomeRevision != expectedOutcomeRevision ||
		!operation.Acknowledged {
		return updaterapi.Operation{}, false
	}
	return operation, true
}

func mapExecutorAcknowledgeError(err error) error {
	return mapExecutorCallError(err, "acknowledge host upgrade", executorCallMessages{
		unavailable: "host updater is unavailable; the dismissal was not recorded",
		unconfirmed: "host updater did not confirm the dismissal; retry to make sure it sticks",
		canceled:    "host updater canceled the dismissal request before confirming it",
	}, func(httpErr *updaterapi.HTTPError) error {
		switch httpErr.StatusCode {
		case http.StatusNotFound:
			if httpErr.Code == updaterapi.ErrorCodeOperationNotFound {
				return fleeterror.NewNotFoundError(httpErr.Message)
			}
		case http.StatusConflict, http.StatusBadRequest:
			return fleeterror.NewFailedPreconditionError(httpErr.Message)
		}
		return fmt.Errorf("host updater rejected acknowledgement with HTTP status %d", httpErr.StatusCode)
	})
}

func (s *Service) logUpgradeAcknowledged(ctx context.Context, organizationID int64, operation updaterapi.Operation) {
	s.logUpgradeEvent(ctx, organizationID, upgradeAcknowledgedEventType,
		fmt.Sprintf("Dismissed the %s outcome of the upgrade to %s", operation.Phase, operation.TargetVersion),
		map[string]any{
			"operation_id":     operation.ID,
			"outcome_revision": operation.OutcomeRevision,
			"target_version":   operation.TargetVersion,
			"phase":            string(operation.Phase),
		}, upgradeAcknowledgementIdempotencyKey(organizationID, operation))
}

func upgradeOperationIdempotencyKey(eventType string, organizationID int64, operation updaterapi.Operation) string {
	return fmt.Sprintf(
		"%s:org:%d:operation:%s:started:%s",
		eventType,
		organizationID,
		operation.ID,
		operation.StartedAt.UTC().Format(time.RFC3339Nano),
	)
}

func upgradeAcknowledgementIdempotencyKey(organizationID int64, operation updaterapi.Operation) string {
	return fmt.Sprintf(
		"%s:outcome:%d",
		upgradeOperationIdempotencyKey(upgradeAcknowledgedEventType, organizationID, operation),
		operation.OutcomeRevision,
	)
}

func validateOperationID(operationID string) error {
	parsed, err := uuid.Parse(operationID)
	if err != nil || parsed == uuid.Nil || parsed.String() != operationID {
		return fleeterror.NewInvalidArgumentError("operation id must be a canonical UUID")
	}
	return nil
}

func (s *Service) GetUpgradeStatus(ctx context.Context) UpgradeStatus {
	if s.executor == nil {
		return UpgradeStatus{}
	}
	statusCtx, cancel := context.WithTimeout(ctx, executorStatusTimeout)
	defer cancel()
	status, err := s.executor.Status(statusCtx)
	if err != nil {
		return UpgradeStatus{}
	}
	return UpgradeStatus{ExecutorAvailable: true, Operation: status.Operation}
}

// SetReleaseChannel persists the org's release channel. Values outside the
// two known channels are rejected as invalid argument (the handler already
// screens the proto enum; this keeps the DB CHECK constraint unreachable).
func (s *Service) SetReleaseChannel(ctx context.Context, organizationID int64, channel Channel) error {
	switch channel {
	case ChannelStable, ChannelStableAndRC:
	default:
		return fleeterror.NewInvalidArgumentErrorf("unknown release channel %q", channel)
	}
	if _, err := s.queries.UpsertReleaseChannelSetting(ctx, sqlc.UpsertReleaseChannelSettingParams{
		OrganizationID: organizationID,
		Channel:        string(channel),
	}); err != nil {
		return fmt.Errorf("upsert release channel setting: %w", err)
	}
	orgID := organizationID
	event := activitymodels.Event{
		Category:       activitymodels.CategorySystem,
		Type:           "release_channel_updated",
		OrganizationID: &orgID,
		Description:    fmt.Sprintf("Changed instance release channel to %s", channel),
		Metadata:       map[string]any{"channel": channel},
	}
	activity.StampActor(ctx, &event)
	s.activitySvc.Log(ctx, event)
	return nil
}

// releaseChannel loads the org's channel; a missing row reads as the stable
// default (no row is ever seeded — see the GetReleaseChannelSetting query).
func (s *Service) releaseChannel(ctx context.Context, organizationID int64) (Channel, error) {
	row, err := s.queries.GetReleaseChannelSetting(ctx, organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return ChannelStable, nil
	}
	if err != nil {
		return "", fmt.Errorf("get release channel setting: %w", err)
	}
	switch channel := Channel(row.Channel); channel {
	case ChannelStable, ChannelStableAndRC:
		return channel, nil
	default:
		// Unreachable while the CHECK constraint holds; fail toward the
		// conservative channel rather than erroring a read-only status call.
		return ChannelStable, nil
	}
}

// channelStatusAvailable reports whether the snapshot is complete enough for
// the selected channel. Stable+RC can still make a useful offer from a fresh RC
// when stable discovery is unavailable, but an incomplete RC view makes that
// broader channel unavailable.
func channelStatusAvailable(channel Channel, snap Snapshot, currentVersion string) bool {
	_, stableAvailable := snap.EligibleStable()
	rc, rcAvailable := snap.EligibleRC()
	switch channel {
	case ChannelStable:
		return stableAvailable
	case ChannelStableAndRC:
		if !rcAvailable {
			return false
		}
		if stableAvailable {
			return true
		}
		return rc != nil && semver.Compare(rc.Version, currentVersion) > 0
	default:
		return false
	}
}

// eligibleCandidate picks only verified channel candidates from the snapshot:
// stable sees the latest available stable; stable_and_rc takes the semver max
// of that stable and the latest available RC.
func eligibleCandidate(channel Channel, snap Snapshot) *Release {
	candidate, _ := snap.EligibleStable()
	if channel == ChannelStableAndRC {
		if rc, available := snap.EligibleRC(); available {
			candidate = semverMax(candidate, rc)
		}
	}
	return candidate
}

func semverMax(a, b *Release) *Release {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if semver.Compare(b.Version, a.Version) > 0 {
		return b
	}
	return a
}

func isCanonicalReleaseTag(tag string) bool {
	return isCanonicalStableTag(tag) || isCanonicalRCTag(tag)
}

// installCommand composes the copy-paste upgrade invocation from two
// independently constrained values. The configured base must exactly match
// the trusted Proto Fleet release path, and the GitHub-sourced tag must be
// a canonical stable or RC release tag. The command uses the canonical
// constant rather than the raw config value, so callers cannot bypass
// Config.Validate and interpolate shell syntax.
func installCommand(configuredBaseURL, tag string) (string, bool) {
	if configuredBaseURL != downloadBaseURL || !isCanonicalReleaseTag(tag) {
		return "", false
	}
	installerURL, err := url.JoinPath(downloadBaseURL, tag, "install.sh")
	if err != nil {
		return "", false
	}
	return fmt.Sprintf(`bash <(curl -fsSL "%s") %s`, installerURL, tag), true
}
