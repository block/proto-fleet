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
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
)

// CommandDispatcher is the slice of the command service the rollout domain uses.
type CommandDispatcher interface {
	FirmwareUpdate(ctx context.Context, deviceSelector *commandpb.DeviceSelector, firmwareFileID string) (*command.CommandResult, error)
}

// FirmwareFiles is the slice of the firmware files service the rollout domain uses.
type FirmwareFiles interface {
	GetFirmwareMetadata(fileID string) (files.FirmwareMetadata, error)
}

// ActivityLogger records rollout lifecycle events in the activity log.
type ActivityLogger interface {
	Log(ctx context.Context, event activitymodels.Event)
}

// Service implements release channel management and firmware enforcement.
type Service struct {
	store    *sqlstores.SQLReleaseChannelStore
	tx       interfaces.Transactor
	commands CommandDispatcher
	files    FirmwareFiles
	activity ActivityLogger
	now      func() time.Time
	// shuffle reorders miners for OrderRandom; swapped in tests.
	shuffle func(n int, swap func(i, j int))
}

// NewService builds the rollout service. activityLog may be nil.
func NewService(store *sqlstores.SQLReleaseChannelStore, tx interfaces.Transactor, commands CommandDispatcher, firmwareFiles FirmwareFiles, activityLog ActivityLogger) *Service {
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

// ChannelMiner is a channel member with its currently reported model and firmware.
type ChannelMiner struct {
	DeviceID         int64
	DeviceIdentifier string
	Model            string
	FirmwareVersion  string
	// Another channel's scope also covers this miner; this channel won by
	// specificity.
	Conflicted bool
}

// ModelGroup is the per-model summary inside a channel. Members are listed
// separately with ListChannelMiners.
type ModelGroup struct {
	Model           string
	FirmwareFileID  string
	FirmwareVersion string
	// MinerCount is how many members report this model.
	MinerCount int32
	// OnTargetCount is how many members report the assigned version; 0
	// when nothing is assigned.
	OnTargetCount int32
	// ReportedVersions are the distinct versions members report, sorted.
	ReportedVersions []string
	ActiveRolloutID  int64
}

// Channel is the operator-facing view of a release channel.
type Channel struct {
	ID          int64
	Name        string
	Description string
	Scope       Scope
	Behavior    Behavior
	ModelGroups []ModelGroup
	MinerCount  int32
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ChannelSpec is everything an operator sets on a channel.
type ChannelSpec struct {
	Name        string
	Description string
	Scope       Scope
	Behavior    Behavior
}

// ModelCount is how many miners of one model a scope covers.
type ModelCount struct {
	Model      string
	MinerCount int32
}

// ScopeConflict is another channel whose scope already covers miners the
// previewed scope would take.
type ScopeConflict struct {
	ChannelID   int64
	ChannelName string
	MinerCount  int32
}

// ScopePreview is what a scope resolves to right now.
type ScopePreview struct {
	MinerCount int32
	Models     []ModelCount
	Conflicts  []ScopeConflict
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
		q := s.store.Queries(ctx)
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
			MaxConcurrentOffline:         b.MaxConcurrentOffline,
		})
		if err != nil {
			if sqlstores.IsUniqueViolation(err) {
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
		q := s.store.Queries(ctx)
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
			MaxConcurrentOffline:         b.MaxConcurrentOffline,
		}); err != nil {
			if sqlstores.IsUniqueViolation(err) {
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
	n, err := s.store.Queries(ctx).DeleteReleaseChannel(ctx, sqlc.DeleteReleaseChannelParams{ChannelID: channelID, OrgID: orgID})
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
	q := s.store.Queries(ctx)
	if err := q.DeleteReleaseChannelTargets(ctx, channelID); err != nil {
		return fleeterror.NewInternalErrorf("clear channel targets: %v", err)
	}
	var deviceIDs []int64
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
		for _, d := range devices {
			deviceIDs = append(deviceIDs, d.ID)
		}
	}
	types, ids := scope.targets(deviceIDs)
	if len(ids) == 0 {
		return nil
	}
	if err := q.InsertReleaseChannelTargets(ctx, sqlc.InsertReleaseChannelTargetsParams{
		ChannelID: channelID, TargetTypes: types, TargetIds: ids,
	}); err != nil {
		return fleeterror.NewInternalErrorf("save channel targets: %v", err)
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
	rows, err := s.store.Queries(ctx).ResolveReleaseChannelScope(ctx, sqlc.ResolveReleaseChannelScopeParams{
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
	models := map[string]*ModelCount{}
	conflicts := map[int64]*ScopeConflict{}
	for _, r := range rows {
		preview.MinerCount++
		m, ok := models[r.Model]
		if !ok {
			m = &ModelCount{Model: r.Model}
			models[r.Model] = m
		}
		m.MinerCount++
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
	sort.Slice(preview.Models, func(i, j int) bool { return preview.Models[i].Model < preview.Models[j].Model })
	for _, c := range conflicts {
		preview.Conflicts = append(preview.Conflicts, *c)
	}
	sort.Slice(preview.Conflicts, func(i, j int) bool { return preview.Conflicts[i].ChannelID < preview.Conflicts[j].ChannelID })
	return preview, nil
}

// ListChannels returns all channels of an org with members grouped by model.
func (s *Service) ListChannels(ctx context.Context, orgID int64) ([]Channel, error) {
	rows, err := s.store.Queries(ctx).ListReleaseChannels(ctx, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list channels: %v", err)
	}
	return s.buildChannels(ctx, orgID, rows)
}

// GetChannel returns one channel of an org.
func (s *Service) GetChannel(ctx context.Context, orgID, channelID int64) (*Channel, error) {
	row, err := s.store.Queries(ctx).GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID})
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
// identifier, optionally restricted to one model. The returned cursor is
// empty on the last page.
func (s *Service) ListChannelMiners(ctx context.Context, orgID, channelID int64, model string, pageSize int32, cursor string) ([]ChannelMiner, string, error) {
	q := s.store.Queries(ctx)
	if _, err := q.GetReleaseChannel(ctx, sqlc.GetReleaseChannelParams{ChannelID: channelID, OrgID: orgID}); err != nil {
		return nil, "", fleeterror.NewNotFoundErrorf("release channel not found: %d", channelID)
	}
	params := sqlc.ListReleaseChannelMinersPageParams{ChannelID: channelID, OrgID: orgID}
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
			DeviceID:         m.DeviceID,
			DeviceIdentifier: m.DeviceIdentifier,
			Model:            m.Model,
			FirmwareVersion:  m.FirmwareVersion,
			Conflicted:       m.Conflicted,
		})
	}
	return miners, next, nil
}

const (
	// DefaultPageSize is how many rows a paged list returns when the caller
	// does not ask for a page size.
	DefaultPageSize = 100
	// MaxPageSize caps a requested page size.
	MaxPageSize = 1000
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
// members, assignments and active rollouts.
func (s *Service) buildChannels(ctx context.Context, orgID int64, rows []sqlc.ReleaseChannel) ([]Channel, error) {
	q := s.store.Queries(ctx)
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
	active, err := q.ListActiveFirmwareRollouts(ctx)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("list active rollouts: %v", err)
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
	activeByChannel := map[int64][]sqlc.FirmwareRollout{}
	for _, r := range active {
		activeByChannel[r.FirmwareRollout.ChannelID] = append(activeByChannel[r.FirmwareRollout.ChannelID], r.FirmwareRollout)
	}

	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		groups := map[string]*ModelGroup{}
		order := []string{}
		group := func(model string) *ModelGroup {
			if g, ok := groups[model]; ok {
				return g
			}
			g := &ModelGroup{Model: model}
			groups[model] = g
			order = append(order, model)
			return g
		}
		ch := Channel{
			ID:          row.ID,
			Name:        row.Name,
			Description: row.Description,
			Scope:       scopeFromTargets(targetsByChannel[row.ID]),
			Behavior:    behaviorFromChannel(row),
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		}
		for _, f := range firmwareByChannel[row.ID] {
			g := group(f.Model)
			g.FirmwareFileID = f.FirmwareFileID
			g.FirmwareVersion = f.FirmwareVersion
		}
		versions := map[string]map[string]bool{}
		for _, m := range membersByChannel[row.ID] {
			g := group(m.Model)
			g.MinerCount++
			if g.FirmwareVersion != "" && m.FirmwareVersion == g.FirmwareVersion {
				g.OnTargetCount++
			}
			if versions[m.Model] == nil {
				versions[m.Model] = map[string]bool{}
			}
			versions[m.Model][m.FirmwareVersion] = true
			ch.MinerCount++
		}
		for model, seen := range versions {
			g := group(model)
			for v := range seen {
				g.ReportedVersions = append(g.ReportedVersions, v)
			}
			sort.Strings(g.ReportedVersions)
		}
		for _, r := range activeByChannel[row.ID] {
			group(r.Model).ActiveRolloutID = r.ID
		}
		sort.Strings(order)
		for _, model := range order {
			ch.ModelGroups = append(ch.ModelGroups, *groups[model])
		}
		channels = append(channels, ch)
	}
	return channels, nil
}
