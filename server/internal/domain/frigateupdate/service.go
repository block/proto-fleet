// Package frigateupdate drives Fleet-triggered Frigate software updates for CT1
// containers. A container is an HA pair of Frigate controllers. Fleet stores
// uploaded, signature-checked Debian packages, stages one on every controller,
// asks Frigate to start a self-coordinated rollout, and then tracks that rollout
// to a terminal state. The controllers' root helpers perform the stop, install,
// migrate and start barriers themselves; Fleet never reaches a helper directly.
package frigateupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Job states stored in frigate_update_job.state.
const (
	StateStaging   = "staging"
	StateStaged    = "staged"
	StateStarting  = "starting"
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
	StateVerified  = "verified"
	StateAbandoned = "abandoned"
)

// Per-instance staging states stored in frigate_update_instance.stage_state.
const (
	StagePending = "pending"
	StageStaging = "staging"
	StageStaged  = "staged"
	StageFailed  = "failed"
)

// Fleet-originated error codes. They share the open error_code vocabulary with
// Frigate's UPDATE_* reasons.
const (
	CodePackageMissing          = "FLEET_PACKAGE_MISSING"
	CodeStageFailed             = "FLEET_STAGE_FAILED"
	CodeStageStalled            = "FLEET_STAGE_STALLED"
	CodeStageDigestMismatch     = "FLEET_STAGE_DIGEST_MISMATCH"
	CodeControllerUnsupported   = "FLEET_CONTROLLER_UNSUPPORTED"
	CodeControllerUnreachable   = "FLEET_CONTROLLER_UNREACHABLE"
	CodeInstanceUnreachable     = "FLEET_INSTANCE_UNREACHABLE"
	CodeNoInstances             = "FLEET_NO_INSTANCES"
	CodeUpdateInProgress        = "FLEET_UPDATE_IN_PROGRESS"
	CodeHelperUnavailable       = "FLEET_HELPER_UNAVAILABLE"
	CodeUnsupportedInstallation = "FLEET_UNSUPPORTED_INSTALLATION"
	CodeSelfCoordination        = "FLEET_SELF_COORDINATION_UNSUPPORTED"
	CodeRemoteTriggerDisabled   = "FLEET_REMOTE_TRIGGER_DISABLED"
	CodeClusterMismatch         = "FLEET_CLUSTER_MISMATCH"
	CodeEtcdUnhealthy           = "FLEET_ETCD_UNHEALTHY"
	CodeArchitectureMismatch    = "FLEET_ARCHITECTURE_MISMATCH"
	CodeReleaseKeyUntrusted     = "FLEET_RELEASE_KEY_UNTRUSTED"
	CodeInsufficientSpace       = "FLEET_INSUFFICIENT_SPACE"
	CodeRolloutLost             = "FLEET_ROLLOUT_NOT_FOUND"
	CodeStartTimeout            = "FLEET_START_TIMEOUT"
	CodeControllersLost         = "FLEET_CONTROLLERS_LOST"
)

const (
	frigateNotFoundReason = "UPDATE_NOT_FOUND"

	actionRetry  = "retry"
	actionCancel = "cancel"
)

// Authorizer enforces controller:software_update. It is implemented next to the
// handlers, which own the middleware dependency, so the domain stays free of it.
type Authorizer interface {
	// AuthorizeSite requires the permission at a container's site (nil for a
	// container without one).
	AuthorizeSite(ctx context.Context, siteID *int64) error
	// AuthorizeAnySite requires the permission organization-wide or at any site.
	// It gates package upload and listing, which are not site resources.
	AuthorizeAnySite(ctx context.Context) error
	// AuthorizeOrgWide requires the permission organization-wide. It gates
	// package deletion because packages are shared by every site.
	AuthorizeOrgWide(ctx context.Context) error
}

// MinerGetter resolves the live miner handle for a device.
type MinerGetter interface {
	GetMinerForCommand(ctx context.Context, deviceID int64) (interfaces.Miner, error)
}

// Service owns update packages and jobs.
type Service struct {
	conn     *sql.DB
	queries  sqlc.Querier
	miners   MinerGetter
	store    *files.FrigatePackageStore
	verifier *frigaterelease.Verifier
	auth     Authorizer
	activity *activity.Service
	cfg      Config
	now      func() time.Time

	mu       sync.Mutex
	inflight map[uuid.UUID]struct{}
	progress map[uuid.UUID]stallClock
	lost     map[uuid.UUID]int
	// dispatched orders runnable jobs by when the runner last handed each to a
	// worker slot; see fairOrder.
	dispatched  map[uuid.UUID]uint64
	dispatchSeq uint64

	// afterStartLease, when set by tests, runs between taking the start lease
	// and sending Start.
	afterStartLease func()
}

// NewService creates the service. verifier may be nil: Fleet then has no
// installed release key, and checks an uploaded release as checkRelease
// describes. activitySvc may be nil.
func NewService(
	conn *sql.DB,
	miners MinerGetter,
	store *files.FrigatePackageStore,
	verifier *frigaterelease.Verifier,
	auth Authorizer,
	activitySvc *activity.Service,
	cfg Config,
) *Service {
	return &Service{
		conn:       conn,
		queries:    db.NewFailoverResettingQuerier(db.NewRetryDB(conn)),
		miners:     miners,
		store:      store,
		verifier:   verifier,
		auth:       auth,
		activity:   activitySvc,
		cfg:        cfg.withDefaults(),
		now:        time.Now,
		inflight:   make(map[uuid.UUID]struct{}),
		progress:   make(map[uuid.UUID]stallClock),
		lost:       make(map[uuid.UUID]int),
		dispatched: make(map[uuid.UUID]uint64),
	}
}

// MaxPackageSize is the largest package Fleet accepts.
func (s *Service) MaxPackageSize() int64 { return s.cfg.MaxPackageSize }

// principal returns the authenticated caller. Update calls act with the
// caller's identity, so a synthesized internal actor is refused.
func principal(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.UserID == 0 || info.OrganizationID == 0 || info.Actor != "" {
		return nil, fleeterror.NewForbiddenError("an authenticated user is required")
	}
	return info, nil
}

// actorName is the Fleet user recorded in Frigate's audit trail.
func actorName(username, externalID string, userID int64) string {
	switch {
	case username != "":
		return username
	case externalID != "":
		return externalID
	default:
		return fmt.Sprintf("user-%d", userID)
	}
}

func siteArg(site sql.NullInt64) *int64 {
	if !site.Valid {
		return nil
	}
	v := site.Int64
	return &v
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

// updaterFor resolves the controller's Frigate update surface.
func (s *Service) updaterFor(ctx context.Context, deviceID int64) (interfaces.FrigateUpdater, error) {
	m, err := s.miners.GetMinerForCommand(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	u, ok := m.(interfaces.FrigateUpdater)
	if !ok {
		return nil, fleeterror.NewUnimplementedError("this controller does not support controller software updates")
	}
	return u, nil
}

// refusalError converts a Frigate refusal to a Fleet error that keeps the
// reason code in its message so operators and clients can classify it.
func refusalError(r *sdk.FrigateRefusal) error {
	msg := r.Reason
	if r.Message != "" {
		msg = r.Reason + ": " + r.Message
	}
	return codedError(r.Code.String(), msg)
}

func codedError(code, msg string) error {
	switch code {
	case "InvalidArgument":
		return fleeterror.NewInvalidArgumentError(msg)
	case "NotFound":
		return fleeterror.NewNotFoundError(msg)
	case "AlreadyExists":
		return fleeterror.NewAlreadyExistsError(msg)
	case "Unavailable", "DeadlineExceeded":
		return fleeterror.NewUnavailableErrorf("%s", msg)
	case "PermissionDenied":
		return fleeterror.NewForbiddenError(msg)
	default:
		return fleeterror.NewFailedPreconditionError(msg)
	}
}

// callError maps an error from a Frigate call to the error returned to the
// operator. Refusals keep their reason; other failures are reported as the
// controller being unreachable so the caller may repeat the same request.
func callError(err error) error {
	if refusal := sdk.AsFrigateRefusal(err); refusal != nil {
		return refusalError(refusal)
	}
	var fe fleeterror.FleetError
	if errors.As(err, &fe) {
		return err
	}
	return fleeterror.NewUnavailableErrorf("the controller did not answer: %v", err)
}

// isFinalRefusal reports whether err is a final Frigate refusal rather than a
// transport-level failure the runner should retry.
func isFinalRefusal(err error) bool { return sdk.AsFrigateRefusal(err) != nil }

func isNotFoundRefusal(err error) bool {
	r := sdk.AsFrigateRefusal(err)
	return r != nil && r.Reason == frigateNotFoundReason
}

// truncate bounds text stored from a controller or an error to limit bytes.
// It cuts on a rune boundary, so the result is always valid UTF-8 that
// PostgreSQL accepts.
func truncate(s string, limit int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

const maxStoredMessage = 1024

// derivedKey is the Frigate idempotency key for one Fleet-originated request.
// Keys are at most 128 printable ASCII characters.
func derivedKey(kind string, jobID uuid.UUID, counter int32) string {
	if counter <= 0 {
		return fmt.Sprintf("fleet-%s-%s", kind, jobID)
	}
	return fmt.Sprintf("fleet-%s-%s-%d", kind, jobID, counter)
}

// runTx runs fn in a database transaction using the service's connection.
func runTx[T any](ctx context.Context, s *Service, fn func(q sqlc.Querier) (T, error)) (T, error) {
	return db.WithTransaction(ctx, s.conn, fn)
}
