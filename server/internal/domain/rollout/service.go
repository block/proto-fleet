// Package rollout implements firmware release channels and the rollouts that
// enforce them.
//
// A release channel is a scope of miners (sites, buildings, racks, groups,
// individual miners) resolved live against fleet placement, with one
// optional firmware assignment per hardware model. Once a model has an
// assignment, the enforcement loop updates every channel member of that
// model that is not running the assigned version. Each enforcement run for
// one (channel, model) pair is tracked as a rollout.
//
// Rollouts run with the channel's behavior: all at once, in batches
// (optionally reviewed after each one), or a pilot batch followed by a
// review gate and the rest. A gate can be released manually or, when the
// channel opted in, automatically once post-update evidence meets its
// thresholds. A miner counts as updated only when it reports the target
// version, is back online, and is at least as healthy (hashing) as before
// the update; a miner that does not verify after MaxAttempts commands is
// failed and left alone until an operator retries it.
package rollout

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	commandpb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// CommandDispatcher is the slice of the command service the rollout domain uses.
type CommandDispatcher interface {
	FirmwareUpdateArtifact(ctx context.Context, deviceSelector *commandpb.DeviceSelector, checksum string, metadata files.FirmwareMetadata) (*command.CommandResult, error)
}

// FirmwareFiles is the slice of the firmware files service the rollout domain
// uses: resolving an uploaded file to the artifact an assignment snapshots,
// and finding which uploaded files still carry an assigned checksum.
type FirmwareFiles interface {
	ResolveFirmwareArtifact(fileID string) (files.FirmwareArtifact, error)
	FindFirmwareFileIDByChecksum(sha256Hex string) (string, bool)
	FirmwareFileIDsByChecksum(sha256Hex string) []string
}

// ActivityLogger records rollout lifecycle events in the activity log.
type ActivityLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
}

// QueryProvider resolves prepared queries for each operation. Implementations
// must honor transaction-bound queries in ctx as well as connection failover.
type QueryProvider interface {
	GetQueries(ctx context.Context) sqlc.Querier
}

// Service implements release channel management and firmware enforcement.
type Service struct {
	store    QueryProvider
	tx       interfaces.Transactor
	commands CommandDispatcher
	files    FirmwareFiles
	activity ActivityLogger
	now      func() time.Time
	// shuffle reorders miners for OrderRandom; swapped in tests.
	shuffle func(n int, swap func(i, j int))
}

// NewService builds the rollout service. activityLog may be nil.
func NewService(store QueryProvider, tx interfaces.Transactor, commands CommandDispatcher, firmwareFiles FirmwareFiles, activityLog ActivityLogger) *Service {
	return &Service{
		store:    store,
		tx:       tx,
		commands: commands,
		files:    firmwareFiles,
		activity: activityLog,
		now:      time.Now,
		shuffle:  defaultShuffle,
	}
}

func defaultShuffle(n int, swap func(i, j int)) { rand.Shuffle(n, swap) }

// ChannelMiner is a channel member with its observed identity, reported
// firmware and managed-deployment provenance.
type ChannelMiner struct {
	DeviceID         int64
	DeviceIdentifier string
	Manufacturer     string
	Model            string
	FirmwareVersion  string
	// Another channel's scope also covers this miner; this channel won by
	// specificity.
	Conflicted bool
	// LastDeployedFirmwareChecksum is the artifact Fleet last deployed to
	// the miner; empty when none is recorded.
	LastDeployedFirmwareChecksum string
}

// ModelGroup is the summary of one observed manufacturer/model pair inside a
// channel, joined to the pair's assignment. Members are listed separately
// with ListChannelMiners; groups are paged with ListChannelModelGroups.
type ModelGroup struct {
	// Manufacturer and Model are the observed identity, verbatim.
	Manufacturer string
	Model        string
	// Assignment, empty when the pair is unassigned. FirmwareFileID is the
	// uploaded file that currently carries FirmwareChecksum, if any.
	FirmwareChecksum           string
	FirmwareVersion            string
	FirmwareTargetManufacturer string
	FirmwareTargetModel        string
	FirmwareFileID             string
	FirmwareAvailable          bool
	AssignmentGeneration       int64
	// MinerCount is how many members report this pair.
	MinerCount int32
	// OnTargetCount is how many members report the assigned version with
	// provenance equal to the assigned checksum; 0 when unassigned.
	OnTargetCount int32
	// ReportedVersions are the first 10 distinct versions members report,
	// sorted; ReportedVersionCount is the total before truncation.
	ReportedVersions     []string
	ReportedVersionCount int32
	ActiveRolloutID      int64
}

// Channel is the operator-facing view of a release channel.
type Channel struct {
	ID          int64
	Name        string
	Description string
	Scope       Scope
	Behavior    Behavior
	// ModelGroupCount counts observed pairs plus assigned pairs with no
	// current members; the groups themselves are paged.
	ModelGroupCount int32
	MinerCount      int32
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MembershipConflict is one (miner, channel) relation for a miner matched by
// several channels, with how the membership rule resolved it.
type MembershipConflict struct {
	DeviceID         int64
	DeviceIdentifier string
	Manufacturer     string
	Model            string
	ChannelID        int64
	ChannelName      string
	// Specificity of the channel's best selector: 1 miner .. 5 site.
	Specificity int32
	// Resolution is winner, loser or excluded_tie.
	Resolution string
}

const (
	// ResolutionWinner and friends are the MembershipConflict outcomes.
	ResolutionWinner      = "winner"
	ResolutionLoser       = "loser"
	ResolutionExcludedTie = "excluded_tie"

	// PreviewListLimit bounds the model and conflict lists of a scope
	// preview; the counts report the totals.
	PreviewListLimit = 100
)

// ChannelSpec is everything an operator sets on a channel.
type ChannelSpec struct {
	Name        string
	Description string
	Scope       Scope
	Behavior    Behavior
}

// ModelCount is how many miners of one observed manufacturer/model pair a
// scope covers.
type ModelCount struct {
	Manufacturer string
	Model        string
	MinerCount   int32
}

// ScopeConflict is another channel whose scope already covers miners the
// previewed scope would take.
type ScopeConflict struct {
	ChannelID   int64
	ChannelName string
	MinerCount  int32
}

// ScopePreview is what a scope resolves to right now. Models and Conflicts
// hold the first PreviewListLimit entries; ModelCount and ConflictCount are
// the totals.
type ScopePreview struct {
	MinerCount    int32
	Models        []ModelCount
	Conflicts     []ScopeConflict
	ModelCount    int32
	ConflictCount int32
}

// --- Channels ---

// CreateChannel creates a release channel. Fails when the scope overlaps
// another channel's.
func (s *Service) CreateChannel(ctx context.Context, orgID, userID int64, spec ChannelSpec) (*Channel, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	var channelID int64
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		q := s.store.GetQueries(ctx)
		if err := q.LockReleaseChannelScopes(ctx, orgID); err != nil {
			return fleeterror.NewInternalErrorf("lock channel scopes: %v", err)
		}
		if err := s.rejectOverlap(ctx, orgID, spec.Scope, 0); err != nil {
			return err
		}
		b := spec.Behavior
		row, err := q.CreateReleaseChannel(ctx, sqlc.CreateReleaseChannelParams{
			OrgID:                        orgID,
			Name:                         spec.Name,
			Description:                  spec.Description,
			CreatedBy:                    userID,
			Method:                       b.Method,
			OrderBy:                      b.Order,
			BatchSize:                    b.BatchSize,
			PilotSize:                    b.PilotSize,
			WaitBetweenBatchesSeconds:    b.WaitBetweenBatchesSeconds,
			ReviewAfterEachBatch:         b.ReviewAfterEachBatch,
			AutoContinue:                 b.AutoContinue,
			StabilizationSeconds:         b.StabilizationSeconds,
			MaxHashrateDropPercent:       toNullFloat(b.Thresholds.MaxHashrateDropPercent),
			MaxEfficiencyIncreasePercent: toNullFloat(b.Thresholds.MaxEfficiencyIncreasePercent),
			MaxTempIncreaseC:             toNullFloat(b.Thresholds.MaxTempIncreaseC),
			MaxNewErrors:                 toNullInt(b.Thresholds.MaxNewErrors),
			MinSampleCoveragePercent:     toNullFloat(b.Thresholds.MinSampleCoveragePercent),
			MaxConcurrentOffline:         b.MaxConcurrentOffline,
			ControllerTimeoutSeconds:     b.ControllerTimeoutSeconds,
		})
		if err != nil {
			if db.IsUniqueViolationError(err) {
				return fleeterror.NewInvalidArgumentErrorf("a release channel named %q already exists", spec.Name)
			}
			return fleeterror.NewInternalErrorf("create channel: %v", err)
		}
		channelID = row.ID
		return s.replaceTargets(ctx, orgID, row.ID, spec.Scope)
	})
	if err != nil {
		return nil, err
	}
	return s.GetChannel(ctx, orgID, channelID)
}

// UpdateChannel replaces a channel's name, description, scope and behavior.
// Rollouts in flight keep the behavior they started with.
func (s *Service) UpdateChannel(ctx context.Context, orgID, channelID int64, spec ChannelSpec) (*Channel, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		q := s.store.GetQueries(ctx)
		if err := q.LockReleaseChannelScopes(ctx, orgID); err != nil {
			return fleeterror.NewInternalErrorf("lock channel scopes: %v", err)
		}
		if _, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID}); err != nil {
			return fleeterror.NewNotFoundErrorf("release channel not found: %d", channelID)
		}
		if err := s.rejectOverlap(ctx, orgID, spec.Scope, channelID); err != nil {
			return err
		}
		b := spec.Behavior
		if _, err := q.UpdateReleaseChannel(ctx, sqlc.UpdateReleaseChannelParams{
			ChannelID:                    channelID,
			OrgID:                        orgID,
			Name:                         spec.Name,
			Description:                  spec.Description,
			Method:                       b.Method,
			OrderBy:                      b.Order,
			BatchSize:                    b.BatchSize,
			PilotSize:                    b.PilotSize,
			WaitBetweenBatchesSeconds:    b.WaitBetweenBatchesSeconds,
			ReviewAfterEachBatch:         b.ReviewAfterEachBatch,
			AutoContinue:                 b.AutoContinue,
			StabilizationSeconds:         b.StabilizationSeconds,
			MaxHashrateDropPercent:       toNullFloat(b.Thresholds.MaxHashrateDropPercent),
			MaxEfficiencyIncreasePercent: toNullFloat(b.Thresholds.MaxEfficiencyIncreasePercent),
			MaxTempIncreaseC:             toNullFloat(b.Thresholds.MaxTempIncreaseC),
			MaxNewErrors:                 toNullInt(b.Thresholds.MaxNewErrors),
			MinSampleCoveragePercent:     toNullFloat(b.Thresholds.MinSampleCoveragePercent),
			MaxConcurrentOffline:         b.MaxConcurrentOffline,
			ControllerTimeoutSeconds:     b.ControllerTimeoutSeconds,
		}); err != nil {
			if db.IsUniqueViolationError(err) {
				return fleeterror.NewInvalidArgumentErrorf("a release channel named %q already exists", spec.Name)
			}
			return fleeterror.NewInternalErrorf("update channel: %v", err)
		}
		return s.replaceTargets(ctx, orgID, channelID, spec.Scope)
	})
	if err != nil {
		return nil, err
	}
	return s.GetChannel(ctx, orgID, channelID)
}

// DeleteChannel removes a channel with its assignments and rollout history.
func (s *Service) DeleteChannel(ctx context.Context, orgID, channelID int64) error {
	n, err := s.store.GetQueries(ctx).DeleteReleaseChannel(ctx, sqlc.DeleteReleaseChannelParams{ChannelID: channelID, OrgID: orgID})
	if err != nil {
		return fleeterror.NewInternalErrorf("delete channel: %v", err)
	}
	if n == 0 {
		return fleeterror.NewNotFoundErrorf("release channel not found: %d", channelID)
	}
	return nil
}

func (spec *ChannelSpec) validate() error {
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		return fleeterror.NewInvalidArgumentError("release channel name is required")
	}
	spec.Description = strings.TrimSpace(spec.Description)
	spec.Scope.normalize()
	return spec.Behavior.validate()
}

func (s *Service) replaceTargets(ctx context.Context, orgID, channelID int64, scope Scope) error {
	q := s.store.GetQueries(ctx)
	if err := q.DeleteReleaseChannelTargets(ctx, channelID); err != nil {
		return fleeterror.NewInternalErrorf("clear channel targets: %v", err)
	}
	if len(scope.DeviceIdentifiers) > 0 {
		devices, err := q.ListDeviceIDsByIdentifiers(ctx, sqlc.ListDeviceIDsByIdentifiersParams{
			OrgID: orgID, DeviceIdentifiers: scope.DeviceIdentifiers,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("resolve miner identifiers: %v", err)
		}
		if len(devices) != len(scope.DeviceIdentifiers) {
			known := map[string]bool{}
			for _, d := range devices {
				known[d.DeviceIdentifier] = true
			}
			for _, id := range scope.DeviceIdentifiers {
				if !known[id] {
					return fleeterror.NewInvalidArgumentErrorf("unknown miner %q", id)
				}
			}
		}
	}
	types, ids := scope.targets()
	if len(ids) > 0 {
		if err := q.InsertReleaseChannelTargets(ctx, sqlc.InsertReleaseChannelTargetsParams{
			ChannelID: channelID, TargetTypes: types, TargetIds: ids,
		}); err != nil {
			return fleeterror.NewInternalErrorf("save channel targets: %v", err)
		}
	}
	if len(scope.DeviceIdentifiers) > 0 {
		if err := q.InsertReleaseChannelMinerTargets(ctx, sqlc.InsertReleaseChannelMinerTargetsParams{
			ChannelID: channelID, DeviceIdentifiers: scope.DeviceIdentifiers,
		}); err != nil {
			return fleeterror.NewInternalErrorf("save channel miner targets: %v", err)
		}
	}
	return nil
}

// rejectOverlap fails when any miner the scope covers is already claimed by
// another channel. Must run under LockReleaseChannelScopes.
func (s *Service) rejectOverlap(ctx context.Context, orgID int64, scope Scope, excludeChannelID int64) error {
	preview, err := s.PreviewScope(ctx, orgID, scope, excludeChannelID)
	if err != nil {
		return err
	}
	if len(preview.Conflicts) == 0 {
		return nil
	}
	parts := make([]string, 0, len(preview.Conflicts))
	for _, c := range preview.Conflicts {
		parts = append(parts, fmt.Sprintf("%s (%d miners)", c.ChannelName, c.MinerCount))
	}
	return fleeterror.NewFailedPreconditionErrorf("scope overlaps release channel %s", strings.Join(parts, ", "))
}

// PreviewScope resolves a scope without saving it: miners per model, and
// the channels it would overlap. excludeChannelID is the channel being
// edited (0 when creating).
func (s *Service) PreviewScope(ctx context.Context, orgID int64, scope Scope, excludeChannelID int64) (*ScopePreview, error) {
	scope.normalize()
	preview := &ScopePreview{}
	if scope.IsEmpty() {
		return preview, nil
	}
	rows, err := s.store.GetQueries(ctx).ResolveReleaseChannelScope(ctx, sqlc.ResolveReleaseChannelScopeParams{
		OrgID:             orgID,
		DeviceIdentifiers: scope.DeviceIdentifiers,
		GroupIds:          scope.GroupIDs,
		RackIds:           scope.RackIDs,
		BuildingIds:       scope.BuildingIDs,
		SiteIds:           scope.SiteIDs,
		ExcludeChannelID:  excludeChannelID,
	})
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("resolve scope: %v", err)
	}
	models := map[PairKey]*ModelCount{}
	conflicts := map[int64]*ScopeConflict{}
	countedMiners := make(map[int64]struct{}, len(rows))
	for _, r := range rows {
		// A miner contributes one row per conflicting channel, but counts
		// only once in the scope and its observed hardware group.
		if _, counted := countedMiners[r.DeviceID]; !counted {
			countedMiners[r.DeviceID] = struct{}{}
			preview.MinerCount++
			key := PairKey{Manufacturer: r.Manufacturer, Model: r.Model}
			m, ok := models[key]
			if !ok {
				m = &ModelCount{Manufacturer: r.Manufacturer, Model: r.Model}
				models[key] = m
			}
			m.MinerCount++
		}
		if r.OwnerChannelID != 0 {
			c, ok := conflicts[r.OwnerChannelID]
			if !ok {
				c = &ScopeConflict{ChannelID: r.OwnerChannelID, ChannelName: r.OwnerChannelName}
				conflicts[r.OwnerChannelID] = c
			}
			c.MinerCount++
		}
	}
	for _, m := range models {
		preview.Models = append(preview.Models, *m)
	}
	sort.Slice(preview.Models, func(i, j int) bool {
		if preview.Models[i].Manufacturer != preview.Models[j].Manufacturer {
			return preview.Models[i].Manufacturer < preview.Models[j].Manufacturer
		}
		return preview.Models[i].Model < preview.Models[j].Model
	})
	for _, c := range conflicts {
		preview.Conflicts = append(preview.Conflicts, *c)
	}
	sort.Slice(preview.Conflicts, func(i, j int) bool { return preview.Conflicts[i].ChannelID < preview.Conflicts[j].ChannelID })
	preview.ModelCount = int32(len(preview.Models))       //nolint:gosec // bounded by the fleet size
	preview.ConflictCount = int32(len(preview.Conflicts)) //nolint:gosec // bounded by the channel count
	if len(preview.Models) > PreviewListLimit {
		preview.Models = preview.Models[:PreviewListLimit]
	}
	if len(preview.Conflicts) > PreviewListLimit {
		preview.Conflicts = preview.Conflicts[:PreviewListLimit]
	}
	return preview, nil
}

// ListChannels returns all channels of an org with their member and group
// counts.
func (s *Service) ListChannels(ctx context.Context, orgID int64) ([]Channel, error) {
	rows, err := s.store.GetQueries(ctx).ListReleaseChannels(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list channels: %v", err)
	}
	return s.buildChannels(ctx, orgID, rows)
}

// GetChannel returns one channel of an org.
func (s *Service) GetChannel(ctx context.Context, orgID, channelID int64) (*Channel, error) {
	row, err := s.store.GetQueries(ctx).GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID})
	if err != nil {
		return nil, fleeterror.NewNotFoundErrorf("release channel not found: %d", channelID)
	}
	channels, err := s.buildChannels(ctx, orgID, []sqlc.ReleaseChannel{row})
	if err != nil {
		return nil, err
	}
	return &channels[0], nil
}

// ListChannelMiners returns one page of a channel's members ordered by
// identifier, optionally restricted to an observed manufacturer and/or model
// (matched verbatim). The returned cursor is empty on the last page.
func (s *Service) ListChannelMiners(ctx context.Context, orgID, channelID int64, manufacturer, model string, pageSize int32, cursor string) ([]ChannelMiner, string, error) {
	q := s.store.GetQueries(ctx)
	if _, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID}); err != nil {
		return nil, "", fleeterror.NewNotFoundErrorf("release channel not found: %d", channelID)
	}
	params := sqlc.ListReleaseChannelMinersPageParams{ChannelID: channelID, OrgID: orgID}
	if manufacturer != "" {
		params.Manufacturer = sql.NullString{String: manufacturer, Valid: true}
	}
	if model != "" {
		params.Model = sql.NullString{String: model, Valid: true}
	}
	if cursor != "" {
		parts, err := decodeCursor(cursor, 2)
		if err != nil {
			return nil, "", err
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || id <= 0 {
			return nil, "", fleeterror.NewInvalidArgumentError("invalid cursor")
		}
		params.AfterIdentifier = sql.NullString{String: parts[0], Valid: true}
		params.AfterDeviceID = sql.NullInt64{Int64: id, Valid: true}
	}
	limit := clampPageSize(pageSize)
	params.PageLimit = limit + 1

	rows, err := q.ListReleaseChannelMinersPage(ctx, params)
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("list channel miners: %v", err)
	}
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.DeviceIdentifier, strconv.FormatInt(last.DeviceID, 10))
	}
	miners := make([]ChannelMiner, 0, len(rows))
	for _, m := range rows {
		miners = append(miners, ChannelMiner{
			DeviceID:                     m.DeviceID,
			DeviceIdentifier:             m.DeviceIdentifier,
			Manufacturer:                 m.Manufacturer,
			Model:                        m.Model,
			FirmwareVersion:              m.FirmwareVersion,
			Conflicted:                   m.Conflicted,
			LastDeployedFirmwareChecksum: m.LastDeployedFirmwareChecksum,
		})
	}
	return miners, next, nil
}

// ListChannelModelGroups returns one page of a channel's manufacturer/model
// groups sorted by manufacturer then model: every observed pair among its
// members plus every assigned pair with no current members. The returned
// cursor is empty on the last page.
func (s *Service) ListChannelModelGroups(ctx context.Context, orgID, channelID int64, pageSize int32, cursor string) ([]ModelGroup, string, error) {
	q := s.store.GetQueries(ctx)
	if _, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID}); err != nil {
		return nil, "", fleeterror.NewNotFoundErrorf("release channel not found: %d", channelID)
	}
	params := sqlc.ListReleaseChannelModelGroupsPageParams{ChannelID: channelID, OrgID: orgID}
	if cursor != "" {
		parts, err := decodeCursor(cursor, 2)
		if err != nil {
			return nil, "", err
		}
		params.AfterManufacturer = sql.NullString{String: parts[0], Valid: true}
		params.AfterModel = sql.NullString{String: parts[1], Valid: true}
	}
	limit := min(clampPageSize(pageSize), ModelGroupPageSize)
	params.PageLimit = limit + 1

	rows, err := q.ListReleaseChannelModelGroupsPage(ctx, params)
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("list channel model groups: %v", err)
	}
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.Manufacturer, last.Model)
	}
	groups := make([]ModelGroup, 0, len(rows))
	for _, r := range rows {
		g := ModelGroup{
			Manufacturer:               r.Manufacturer,
			Model:                      r.Model,
			FirmwareChecksum:           r.FirmwareChecksum,
			FirmwareVersion:            r.FirmwareVersion,
			FirmwareTargetManufacturer: r.FirmwareTargetManufacturer,
			FirmwareTargetModel:        r.FirmwareTargetModel,
			AssignmentGeneration:       r.AssignmentGeneration,
			MinerCount:                 r.MinerCount,
			OnTargetCount:              r.OnTargetCount,
			ReportedVersions:           r.ReportedVersions,
			ReportedVersionCount:       r.ReportedVersionCount,
			ActiveRolloutID:            r.ActiveRolloutID,
		}
		if g.FirmwareChecksum != "" {
			g.FirmwareFileID, g.FirmwareAvailable = s.files.FindFirmwareFileIDByChecksum(g.FirmwareChecksum)
		}
		groups = append(groups, g)
	}
	return groups, next, nil
}

// ListMembershipConflicts returns one page of (miner, channel) relations for
// miners matched by several channels, ordered by identifier then channel,
// optionally restricted to one channel. The returned cursor is empty on the
// last page.
func (s *Service) ListMembershipConflicts(ctx context.Context, orgID, channelID int64, pageSize int32, cursor string) ([]MembershipConflict, string, error) {
	params := sqlc.ListReleaseChannelMembershipConflictsPageParams{OrgID: orgID}
	if channelID != 0 {
		params.ChannelID = sql.NullInt64{Int64: channelID, Valid: true}
	}
	if cursor != "" {
		parts, err := decodeCursor(cursor, 3)
		if err != nil {
			return nil, "", err
		}
		deviceID, err := strconv.ParseInt(parts[1], 10, 64)
		afterChannel, err2 := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || err2 != nil || deviceID <= 0 || afterChannel <= 0 {
			return nil, "", fleeterror.NewInvalidArgumentError("invalid cursor")
		}
		params.AfterIdentifier = sql.NullString{String: parts[0], Valid: true}
		params.AfterDeviceID = sql.NullInt64{Int64: deviceID, Valid: true}
		params.AfterChannelID = sql.NullInt64{Int64: afterChannel, Valid: true}
	}
	limit := min(clampPageSize(pageSize), ModelGroupPageSize)
	params.PageLimit = limit + 1

	rows, err := s.store.GetQueries(ctx).ListReleaseChannelMembershipConflictsPage(ctx, params)
	if err != nil {
		return nil, "", fleeterror.NewInternalErrorf("list membership conflicts: %v", err)
	}
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.DeviceIdentifier, strconv.FormatInt(last.DeviceID, 10), strconv.FormatInt(last.ChannelID, 10))
	}
	conflicts := make([]MembershipConflict, 0, len(rows))
	for _, r := range rows {
		conflicts = append(conflicts, MembershipConflict{
			DeviceID:         r.DeviceID,
			DeviceIdentifier: r.DeviceIdentifier,
			Manufacturer:     r.Manufacturer,
			Model:            r.Model,
			ChannelID:        r.ChannelID,
			ChannelName:      r.ChannelName,
			Specificity:      r.Specificity,
			Resolution:       r.Resolution,
		})
	}
	return conflicts, next, nil
}

const (
	// DefaultPageSize is how many rows a paged list returns when the caller
	// does not ask for a page size.
	DefaultPageSize = 100
	// MaxPageSize caps a requested page size.
	MaxPageSize = 1000
	// ModelGroupPageSize caps model group and conflict pages, whose rows
	// carry long identity strings.
	ModelGroupPageSize = 100
)

func clampPageSize(n int32) int32 {
	switch {
	case n <= 0:
		return DefaultPageSize
	case n > MaxPageSize:
		return MaxPageSize
	}
	return n
}

// Cursors encode the sort key of the last row on a page so the next page
// resumes strictly after it; they are opaque to callers.
func encodeCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x00")))
}

func decodeCursor(cursor string, n int) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("invalid cursor")
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != n {
		return nil, fleeterror.NewInvalidArgumentError("invalid cursor")
	}
	return parts, nil
}

// buildChannels assembles channel views from one load of the org's targets,
// members and assignments. Groups are paged separately; the channel carries
// their count: observed pairs plus assigned pairs with no current members.
func (s *Service) buildChannels(ctx context.Context, orgID int64, rows []sqlc.ReleaseChannel) ([]Channel, error) {
	q := s.store.GetQueries(ctx)
	targets, err := q.ListReleaseChannelTargets(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list channel targets: %v", err)
	}
	members, err := q.ListReleaseChannelMembers(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list channel members: %v", err)
	}
	firmware, err := q.ListReleaseChannelFirmware(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list channel firmware: %v", err)
	}

	targetsByChannel := map[int64][]sqlc.ListReleaseChannelTargetsRow{}
	for _, t := range targets {
		targetsByChannel[t.ChannelID] = append(targetsByChannel[t.ChannelID], t)
	}
	membersByChannel := map[int64][]sqlc.ListReleaseChannelMembersRow{}
	for _, m := range members {
		membersByChannel[m.ChannelID] = append(membersByChannel[m.ChannelID], m)
	}
	firmwareByChannel := map[int64][]sqlc.ReleaseChannelFirmware{}
	for _, f := range firmware {
		firmwareByChannel[f.ChannelID] = append(firmwareByChannel[f.ChannelID], f)
	}

	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		ch := Channel{
			ID:          row.ID,
			Name:        row.Name,
			Description: row.Description,
			Scope:       scopeFromTargets(targetsByChannel[row.ID]),
			Behavior:    behaviorFromChannel(row),
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		}
		observed := map[PairKey]bool{}
		folded := map[PairKey]bool{}
		for _, m := range membersByChannel[row.ID] {
			ch.MinerCount++
			observed[PairKey{Manufacturer: m.Manufacturer, Model: m.Model}] = true
			folded[PairKey{Manufacturer: strings.TrimSpace(m.Manufacturer), Model: strings.TrimSpace(m.Model)}.folded()] = true
		}
		ch.ModelGroupCount = int32(len(observed)) //nolint:gosec // bounded by the fleet size
		for _, f := range firmwareByChannel[row.ID] {
			if f.FirmwareChecksum != "" && !folded[PairKey{Manufacturer: f.Manufacturer, Model: f.Model}.folded()] {
				ch.ModelGroupCount++
			}
		}
		channels = append(channels, ch)
	}
	return channels, nil
}
