package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/pools/preflight"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/block/proto-fleet/server/internal/domain/sv2/translator"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/queue"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func (s *Service) prepareUpdateMiningPoolsDispatch(
	ctx context.Context,
	organizationID int64,
	devices []resolvedDevice,
	payload *dto.UpdateMiningPoolsPayload,
) ([]queue.EnqueueMessage, error) {
	slots, sourcePools := poolSlotsFromPayload(payload)
	selectedIdentifiers := make([]string, 0, len(devices))
	for _, device := range devices {
		selectedIdentifiers = append(selectedIdentifiers, device.identifier)
	}
	if !containsSV2(slots) {
		payload.ReleaseSV2Translation = true
		return nil, nil
	}
	if s.pluginCaps == nil {
		return nil, fleeterror.NewInternalError("SV2 miner capability provider is not configured")
	}

	rows, err := s.resolvePoolCapabilityDevices(ctx, organizationID, devices)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("resolve SV2 miner capabilities: %v", err)
	}
	rowByID := make(map[int64]poolCapabilityDevice, len(rows))
	for _, row := range rows {
		rowByID[row.id] = row
	}

	type capabilityKey struct{ driver, manufacturer, model string }
	nativeByType := make(map[capabilityKey]bool)
	planDevices := make([]preflight.Device, 0, len(devices))
	for _, device := range devices {
		row, ok := rowByID[device.id]
		if !ok {
			return nil, fleeterror.NewFailedPreconditionErrorf(
				"cannot determine Stratum V2 capability for miner %s",
				device.identifier,
			)
		}
		key := capabilityKey{row.driver, row.manufacturer, row.model}
		native, cached := nativeByType[key]
		if !cached {
			native = s.pluginCaps.GetRawCapabilitiesForDevice(
				ctx,
				key.driver,
				key.manufacturer,
				key.model,
			)[sdk.CapabilityNativeStratumV2]
			nativeByType[key] = native
		}
		planDevices = append(planDevices, preflight.Device{
			Identifier:      device.identifier,
			NativeStratumV2: native,
		})
	}

	plan, err := preflight.Plan(planDevices, slots)
	if err != nil {
		if errors.Is(err, preflight.ErrNonContiguousSV2Slots) {
			return nil, fleeterror.NewInvalidArgumentError(
				"Stratum V2 pool slots must be adjacent when assigning SV1-only miners",
			)
		}
		return nil, fleeterror.NewInvalidArgumentErrorf("plan Stratum V2 pool assignment: %v", err)
	}
	if !plan.TranslationRequired() {
		payload.ReleaseSV2Translation = true
		return nil, nil
	}
	if s.translatorManager == nil {
		return nil, fleeterror.NewInternalError("SV2 translator manager is not configured")
	}
	translatedIdentifiers := make([]string, 0, len(plan.Devices))
	for _, route := range plan.Devices {
		for _, slot := range route.Slots {
			if slot.UsesTranslation {
				translatedIdentifiers = append(translatedIdentifiers, route.DeviceIdentifier)
				break
			}
		}
	}
	endpoint, err := s.translatorManager.PreviewAssignment(
		ctx,
		&plan.TranslatorProfile,
		translator.Assignment{
			SelectedDeviceIdentifiers:   selectedIdentifiers,
			TranslatedDeviceIdentifiers: translatedIdentifiers,
		},
	)
	if err != nil {
		return nil, fleeterror.NewFailedPreconditionErrorf("plan Stratum V2 translator assignment: %v", err)
	}

	routeByIdentifier := make(map[string]preflight.DeviceRoute, len(plan.Devices))
	for _, route := range plan.Devices {
		routeByIdentifier[route.DeviceIdentifier] = route
	}
	messages := make([]queue.EnqueueMessage, 0, len(devices))
	for _, device := range devices {
		route, ok := routeByIdentifier[device.identifier]
		if !ok {
			return nil, fleeterror.NewInternalErrorf("missing pool route for miner %s", device.identifier)
		}
		devicePayload, err := poolPayloadForRoute(sourcePools, route, endpoint)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("build pool route for miner %s: %v", device.identifier, err)
		}
		if routeUsesTranslation(route) {
			devicePayload.SV2Translation = &dto.SV2TranslationInstruction{
				Profile:               plan.TranslatorProfile,
				TranslatedPoolIndexes: translatedPoolIndexes(route),
			}
		} else {
			devicePayload.ReleaseSV2Translation = true
		}
		messages = append(messages, queue.EnqueueMessage{DeviceID: device.id, Payload: devicePayload})
	}
	return messages, nil
}

func routeUsesTranslation(route preflight.DeviceRoute) bool {
	for _, slot := range route.Slots {
		if slot.UsesTranslation {
			return true
		}
	}
	return false
}

func translatedPoolIndexes(route preflight.DeviceRoute) []int {
	indexes := make([]int, 0, len(route.Slots))
	for index, slot := range route.Slots {
		if slot.UsesTranslation {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func (s *Service) resolvePoolCapabilityDevices(
	ctx context.Context,
	organizationID int64,
	devices []resolvedDevice,
) ([]poolCapabilityDevice, error) {
	if s.resolvePoolCapabilitiesOverride != nil {
		return s.resolvePoolCapabilitiesOverride(ctx, organizationID, devices)
	}
	identifiers := make([]string, 0, len(devices))
	for _, device := range devices {
		identifiers = append(identifiers, device.identifier)
	}
	rows, err := db.WithTransaction(ctx, s.conn, func(q *sqlc.Queries) ([]sqlc.GetDeviceInfoForCapabilityCheckRow, error) {
		return q.GetDeviceInfoForCapabilityCheck(ctx, sqlc.GetDeviceInfoForCapabilityCheckParams{
			DeviceIdentifiers: identifiers,
			OrgID:             organizationID,
		})
	})
	if err != nil {
		return nil, err
	}
	resolved := make([]poolCapabilityDevice, 0, len(rows))
	for _, row := range rows {
		resolved = append(resolved, poolCapabilityDevice{
			id:           row.ID,
			identifier:   row.DeviceIdentifier,
			driver:       row.DriverName,
			manufacturer: row.Manufacturer.String,
			model:        row.Model.String,
		})
	}
	return resolved, nil
}

func poolSlotsFromPayload(payload *dto.UpdateMiningPoolsPayload) ([]preflight.SlotAssignment, []dto.MiningPool) {
	pools := []dto.MiningPool{payload.DefaultPool}
	if payload.Backup1Pool != nil {
		pools = append(pools, *payload.Backup1Pool)
	}
	if payload.Backup2Pool != nil {
		pools = append(pools, *payload.Backup2Pool)
	}
	slots := make([]preflight.SlotAssignment, 0, len(pools))
	for _, pool := range pools {
		slots = append(slots, preflight.SlotAssignment{URL: pool.URL, Username: pool.Username})
	}
	return slots, pools
}

func containsSV2(slots []preflight.SlotAssignment) bool {
	for _, slot := range slots {
		if sv2.IsSV2URL(slot.URL) {
			return true
		}
	}
	return false
}

func poolPayloadForRoute(
	sourcePools []dto.MiningPool,
	route preflight.DeviceRoute,
	endpoint translator.Endpoint,
) (dto.UpdateMiningPoolsPayload, error) {
	effective := make([]dto.MiningPool, 0, len(route.Slots))
	for _, slot := range route.Slots {
		if slot.SourceIndex < 0 || slot.SourceIndex >= len(sourcePools) {
			return dto.UpdateMiningPoolsPayload{}, fmt.Errorf("source slot %d is out of range", slot.SourceIndex)
		}
		pool := sourcePools[slot.SourceIndex]
		// #nosec G115 -- Fleet supports at most three pool slots.
		pool.Priority = uint32(len(effective))
		if slot.UsesTranslation {
			pool.URL = endpoint.String()
		}
		effective = append(effective, pool)
	}
	if len(effective) == 0 {
		return dto.UpdateMiningPoolsPayload{}, errors.New("pool route is empty")
	}
	payload := dto.UpdateMiningPoolsPayload{DefaultPool: effective[0]}
	if len(effective) > 1 {
		payload.Backup1Pool = &effective[1]
	}
	if len(effective) > 2 {
		payload.Backup2Pool = &effective[2]
	}
	return payload, nil
}
