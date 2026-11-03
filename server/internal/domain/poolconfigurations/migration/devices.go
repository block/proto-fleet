package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
)

type DeviceReader interface {
	ReadPools(ctx context.Context, orgID int64, source sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error)
}

type MinerGetter interface {
	GetMinerForCommand(ctx context.Context, deviceID int64) (interfaces.Miner, error)
}

type FirmwareReader struct {
	writes *devicewrite.Service
	miners MinerGetter
}

func NewFirmwareReader(conn *sql.DB, miners MinerGetter) *FirmwareReader {
	return &FirmwareReader{writes: devicewrite.NewService(conn), miners: miners}
}

var errMiningGroups = errors.New("container uses mining groups")

// ReadPools uses the existing physical read admission and binding gates. It
// never obtains a mutation permit or changes firmware settings.
func (r *FirmwareReader) ReadPools(ctx context.Context, orgID int64, source sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error) {
	var pools []interfaces.MinerStoredPool
	err := r.writes.WithHostObservation(ctx, orgID, source.ID, source.Epoch, func(ctx context.Context) error {
		return r.writes.WithBindingObservation(ctx, source.ID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
			miner, err := r.miners.GetMinerForCommand(ctx, source.ID)
			if err != nil {
				return err
			}
			if miner.GetOrgID() != orgID {
				return fleeterror.NewForbiddenError("device organization changed")
			}
			if commonpb.DeviceClass(source.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
				groups, ok := miner.(sdk.DeviceMiningGroups)
				if !ok {
					return fleeterror.NewFailedPreconditionError("container mode is unavailable")
				}
				mode, err := groups.MiningGroups(ctx, sdk.MiningGroupRequest{Action: "observe"})
				if err != nil {
					return err
				}
				if !mode.Supported || mode.Pending || mode.Suspended {
					return fleeterror.NewFailedPreconditionError("container mode is not ready")
				}
				if mode.Enabled {
					return errMiningGroups
				}
			}
			reader, ok := miner.(interfaces.StoredMiningPoolReader)
			if !ok {
				return fleeterror.NewFailedPreconditionError("stored pool settings are unavailable")
			}
			pools, err = reader.GetStoredMiningPools(ctx)
			return err
		})
	})
	return pools, err
}

// DeviceSnapshot contains encrypted slots only. It stays in memory between
// readback and the offline import; it is never written to an export or log.
type DeviceSnapshot struct {
	source    sqlc.ListPoolDeviceImportSourcesRow
	candidate poolconfigurations.ImportCandidate
	reason    string
}

// CollectDevices runs while application writers are stopped, before the import
// transaction. This avoids holding SQL locks over firmware calls or retrying I/O
// inside a retried transaction. Import rechecks the exact binding and ownership.
func CollectDevices(ctx context.Context, q sqlc.Querier, secrets *encrypt.Service, orgID int64, reader DeviceReader) ([]DeviceSnapshot, error) {
	sources, err := q.ListPoolDeviceImportSources(ctx, orgID)
	if err != nil {
		return nil, err
	}
	blocked, err := blockedPoolDevices(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	snapshots := make([]DeviceSnapshot, 0, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("collect device pool settings: %w", err)
		}
		snapshot := DeviceSnapshot{source: source, candidate: poolconfigurations.ImportCandidate{
			Scope:  poolconfigurations.Scope{OrganizationID: orgID, AccountID: source.AccountID},
			Source: poolconfigurations.ImportSource{Kind: "device_readback", ID: strconv.FormatInt(source.ID, 10), Version: uuid.NewString()},
			Name:   "Imported miner " + strconv.FormatInt(source.ID, 10),
		}}
		switch {
		case blocked[source.ID]:
			snapshot.reason = unsettledPoolReason
		case source.Remote:
			snapshot.reason = "Fleet Node equipment cannot be read by the offline importer; assign a complete configuration explicitly."
		case source.State != "host" || len(source.IdentityKeys) == 0:
			snapshot.reason = "Equipment identity or write ownership is unavailable."
		case source.OwnerCount != 1 || source.AccountID < 0:
			snapshot.reason = "Container ownership is mixed or unavailable; no whole-device assignment was adopted."
		default:
			readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			pools, readErr := reader.ReadPools(readCtx, orgID, source)
			cancel()
			if errors.Is(readErr, errMiningGroups) {
				continue
			}
			if readErr != nil {
				snapshot.reason = "Stored pool settings could not be read; reconnect the device or assign a complete configuration."
			} else if len(pools) == 0 {
				continue
			} else {
				snapshot.candidate.Slots, snapshot.reason = importDeviceSlots(secrets, source.WorkerName, pools)
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func importDeviceSlots(secrets *encrypt.Service, worker string, pools []interfaces.MinerStoredPool) ([]poolconfigurations.StoredSlot, string) {
	if len(pools) > 3 {
		return nil, "The device has more than three pools; review its stack before adoption."
	}
	pools = slices.Clone(pools)
	slices.SortFunc(pools, func(a, b interfaces.MinerStoredPool) int { return int(a.Priority) - int(b.Priority) })
	for i, pool := range pools {
		if pool.Priority < 0 || (i > 0 && pool.Priority == pools[i-1].Priority) {
			return nil, "Pool order is ambiguous; review the stack before adoption."
		}
	}
	// Firmware may report unused backup slots explicitly. Trim only known-empty
	// trailing slots, after validating the full ordering; never infer credentials
	// or collapse an interior hole. Retain the primary for normal validation.
	for len(pools) > 1 {
		last := pools[len(pools)-1]
		if last.URL != "" || last.Username != "" || last.Password == nil || *last.Password != "" {
			break
		}
		pools = pools[:len(pools)-1]
	}
	slots := make([]poolconfigurations.StoredSlot, len(pools))
	for i, pool := range pools {
		if pool.Password == nil {
			return nil, "Pool password is unknown; supply the complete configuration before adoption."
		}
		ciphertext, err := secrets.Encrypt([]byte(*pool.Password))
		if err != nil {
			return nil, "Pool credentials could not be encrypted."
		}
		username, template := pool.Username, ""
		// Normalize only the exact saved miner worker suffix, so different miners
		// share one configuration without changing their rendered usernames.
		if worker != "" && strings.HasSuffix(username, "."+worker) && len(username) > len(worker)+1 {
			username = strings.TrimSuffix(username, "."+worker)
			template = "{miner_name}"
		}
		slots[i] = poolconfigurations.StoredSlot{URL: pool.URL, Username: username, WorkerTemplate: template, PasswordCiphertext: ciphertext}
	}
	return slots, ""
}

func prepareDevices(ctx context.Context, q sqlc.Querier, orgID int64, snapshots []DeviceSnapshot, blocked map[int64]bool) ([]poolconfigurations.ImportCandidate, []poolconfigurations.ImportIssue, error) {
	current, err := q.ListPoolDeviceImportSources(ctx, orgID)
	if err != nil {
		return nil, nil, err
	}
	byID := map[int64]sqlc.ListPoolDeviceImportSourcesRow{}
	for _, source := range current {
		byID[source.ID] = source
	}
	var candidates []poolconfigurations.ImportCandidate
	var issues []poolconfigurations.ImportIssue
	for _, snapshot := range snapshots {
		reason := snapshot.reason
		if blocked[snapshot.source.ID] {
			reason = unsettledPoolReason
		}
		if !reflect.DeepEqual(snapshot.source, byID[snapshot.source.ID]) {
			reason = "Equipment ownership, identity, worker name, or existing assignment changed after readback; no assignment was adopted."
		}
		if reason != "" {
			issues = append(issues, poolconfigurations.ImportIssue{Source: snapshot.candidate.Source, Reason: reason})
			continue
		}
		candidates = append(candidates, snapshot.candidate)
	}
	return candidates, issues, nil
}
