package updates

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"golang.org/x/mod/semver"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
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
)

// UpdateStatus is the gated, channel-filtered update offer for one org.
// LatestEligible is nil — and InstallCommand empty — unless UpdateAvailable.
type UpdateStatus struct {
	CurrentVersion  string
	Channel         Channel
	LatestEligible  *Release
	StatusAvailable bool
	UpdateAvailable bool
	InstallCommand  string
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
}

// NewService creates the updates domain service. serverVersion is the running
// fleetd build (the ldflags main.version) — never a client bundle version.
func NewService(cfg Config, serverVersion string, snapshots snapshotProvider, queries channelSettingQuerier) *Service {
	return newService(cfg, serverVersion, snapshots, queries, slog.Default())
}

func newService(cfg Config, serverVersion string, snapshots snapshotProvider, queries channelSettingQuerier, logger *slog.Logger) *Service {
	return &Service{
		cfg:            cfg,
		currentVersion: serverVersion,
		snapshots:      snapshots,
		queries:        queries,
		logger:         logger,
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
	snapshot := s.snapshots.Snapshot()
	status := UpdateStatus{
		CurrentVersion:  s.currentVersion,
		Channel:         channel,
		StatusAvailable: channelStatusAvailable(channel, snapshot),
	}
	if !status.StatusAvailable {
		return status, nil
	}

	if !semver.IsValid(s.currentVersion) {
		s.logger.Debug("update check skipped: running version is not semver", "version", s.currentVersion)
		return status, nil
	}
	candidate := eligibleCandidate(channel, snapshot)
	if candidate == nil || semver.Compare(candidate.Version, s.currentVersion) <= 0 {
		return status, nil
	}
	command, ok := installCommand(s.cfg.DownloadBaseURL, candidate.Version)
	if !ok {
		// Defensive: a candidate the command guard rejects is never offered
		// at all — an offer without a runnable command would be a dead end.
		return status, nil
	}

	eligible := *candidate
	status.LatestEligible = &eligible
	status.UpdateAvailable = true
	status.InstallCommand = command
	return status, nil
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
func channelStatusAvailable(channel Channel, snap Snapshot) bool {
	_, stableAvailable := snap.EligibleStable()
	rc, rcAvailable := snap.EligibleRC()
	switch channel {
	case ChannelStable:
		return stableAvailable
	case ChannelStableAndRC:
		return rcAvailable && (stableAvailable || rc != nil)
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
