import { useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { useModalLiveRefresh } from "./hooks/useModalLiveRefresh";
import type { ComponentAddress, ProtoFleetStatusModalProps } from "./types";
import {
  buildComponentStatusProps,
  getComponentTitle,
  mapErrorComponentTypeToShared,
  transformErrorsForModal,
  transformFleetErrorsToShared,
} from "./utils";
import { ComponentType as ErrorComponentType, type ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import {
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { StartMiningRequestSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useDeviceErrors } from "@/protoFleet/api/useDeviceErrors";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import useRefreshMiners from "@/protoFleet/api/useRefreshMiners";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";

import { variants } from "@/shared/components/Button";
import { StatusModal as SharedStatusModal } from "@/shared/components/StatusModal";
import type { ComponentStatusData, MinerStatusData } from "@/shared/components/StatusModal/types";
import { pushToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { useMinerStatusSummary } from "@/shared/hooks/useStatusSummary";

// Stable empty array to avoid triggering useDeviceErrors internal effects on every render
const EMPTY_DEVICE_IDS: string[] = [];

/**
 * ProtoFleet-specific StatusModal wrapper that integrates with the store
 *
 * This component encapsulates all the integration logic between the ProtoFleet store
 * and the shared StatusModal component. It handles:
 * - Store data fetching and transformation
 * - Component navigation state
 * - Error grouping and formatting
 *
 * @example
 * ```tsx
 * const [isModalOpen, setModalOpen] = useState(false);
 *
 * <ProtoFleetStatusModal
 *   open={isModalOpen}
 *   onClose={() => setModalOpen(false)}
 *   deviceId={minerId}
 * />
 * ```
 */
const ProtoFleetStatusModal = ({
  open,
  onClose,
  deviceId,
  miner,
  componentAddress,
  showBackButton = true,
  onMergeMiners,
}: ProtoFleetStatusModalProps) => {
  const isVisible = open ?? true;

  // Decouple the modal's subject from the filtered page map. A page poll can
  // drop this device from the shared `miners` map (e.g. remediation moves it
  // out of an active status filter); reading the prop directly would blank the
  // modal and then flicker it back when the next live-refresh merge re-adds the
  // device. Holding the last-known snapshot for the current deviceId keeps the
  // modal stable while each tick still refreshes it through the prop.
  const lastKnownMinerRef = useRef<MinerStateSnapshot | undefined>(undefined);
  if (miner?.deviceIdentifier === deviceId) {
    lastKnownMinerRef.current = miner;
  } else if (lastKnownMinerRef.current?.deviceIdentifier !== deviceId) {
    // The modal switched to a different device before its snapshot arrived —
    // drop the previous subject so we never show another device's data.
    lastKnownMinerRef.current = undefined;
  }
  const activeMiner = miner ?? lastKnownMinerRef.current;

  // Component navigation state
  const [component, setComponent] = useState<ComponentAddress | undefined>(componentAddress);

  // Fetch errors for this device when modal is visible
  const modalDeviceIds = useMemo(() => (isVisible && deviceId ? [deviceId] : EMPTY_DEVICE_IDS), [isVisible, deviceId]);
  const { errorsByDevice, refetch: refetchErrors } = useDeviceErrors(modalDeviceIds);

  // Live refresh: while the modal is open, re-poll this miner's snapshot and
  // errors so on-device remediation reflects here without a close/reopen. The
  // freshest snapshot flows back in through the `miner` prop once the parent
  // fleet map is updated via onMergeMiners.
  const { refreshMiners } = useRefreshMiners();
  const handleLiveRefreshTick = useCallback(
    async (signal: AbortSignal) => {
      if (!deviceId) return;
      try {
        const response = await refreshMiners([deviceId], signal);
        // Bail if the modal closed / switched devices while this was in flight —
        // merging now would clobber the newer modal's fresh state in the shared map.
        if (signal.aborted) return;
        if (response.snapshots.length > 0) {
          onMergeMiners?.(response.snapshots);
        }
      } catch {
        // Auth errors are surfaced inside useRefreshMiners; on any failure we keep
        // the last-good snapshot visible and let the next tick retry.
      }
      // Errors refresh independently of the snapshot merge.
      if (!signal.aborted) {
        await refetchErrors();
      }
    },
    [deviceId, refreshMiners, onMergeMiners, refetchErrors],
  );

  useModalLiveRefresh({
    enabled: isVisible && !!deviceId,
    restartKey: deviceId,
    onTick: handleLiveRefreshTick,
  });

  const handleClose = useCallback(() => {
    setComponent(componentAddress);
    onClose();
  }, [componentAddress, onClose]);

  // Derive errors from the local fetch (not the store)
  const allErrors = useMemo(() => (deviceId ? (errorsByDevice[deviceId] ?? []) : []), [errorsByDevice, deviceId]);
  const groupedErrors = useMemo(() => {
    const grouped = {
      hashboard: [] as ErrorMessage[],
      psu: [] as ErrorMessage[],
      fan: [] as ErrorMessage[],
      controlBoard: [] as ErrorMessage[],
      other: [] as ErrorMessage[],
    };
    allErrors.forEach((error) => {
      switch (error.componentType) {
        case ErrorComponentType.HASH_BOARD:
          grouped.hashboard.push(error);
          break;
        case ErrorComponentType.PSU:
          grouped.psu.push(error);
          break;
        case ErrorComponentType.FAN:
          grouped.fan.push(error);
          break;
        case ErrorComponentType.CONTROL_BOARD:
          grouped.controlBoard.push(error);
          break;
        default:
          grouped.other.push(error);
          break;
      }
    });
    return grouped;
  }, [allErrors]);

  // Wake miner functionality
  const { startMining } = useMinerCommand();

  const handleWakeMiner = useCallback(() => {
    if (!deviceId) return;

    const toastId = pushToast({
      message: "Waking miner...",
      status: TOAST_STATUSES.loading,
      longRunning: true,
    });

    const deviceSelector = createDeviceSelector("subset", [deviceId]);
    const startMiningRequest = create(StartMiningRequestSchema, {
      deviceSelector,
    });

    startMining({
      startMiningRequest,
      onSuccess: () => {
        updateToast(toastId, {
          message: "Miner is waking up",
          status: TOAST_STATUSES.success,
        });
        onClose();
      },
      onError: (error) => {
        updateToast(toastId, {
          message: `Failed to wake miner: ${error}`,
          status: TOAST_STATUSES.error,
        });
      },
    });
  }, [deviceId, startMining, onClose]);

  // Transform ProtoFleet errors to shared format for status computation
  const sharedErrors = useMemo(() => transformFleetErrorsToShared(groupedErrors), [groupedErrors]);

  // Determine status flags from DeviceStatus and PairingStatus
  const needsAuthentication = activeMiner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const isOffline = activeMiner?.deviceStatus === DeviceStatus.OFFLINE;
  // When authentication is needed, we can't trust INACTIVE (or MAINTENANCE) status
  // (could be sleeping OR showing as inactive/maintenance because we can't authenticate)
  const isSleeping =
    (activeMiner?.deviceStatus === DeviceStatus.INACTIVE || activeMiner?.deviceStatus === DeviceStatus.MAINTENANCE) &&
    !needsAuthentication;
  const needsMiningPool = activeMiner?.deviceStatus === DeviceStatus.NEEDS_MINING_POOL;

  // Compute summary using shared hook (replaces API-provided summary)
  const summary = useMinerStatusSummary(sharedErrors, isSleeping, isOffline, needsAuthentication, needsMiningPool);

  // getMinerStatus function - returns complete data including config
  const getMinerStatus = useCallback((): MinerStatusData => {
    // Create onClick handler that navigates to component details
    const onClickHandler = (deviceId: string, type: ErrorComponentType, componentId: string) => {
      setComponent({ deviceId, componentType: type, componentId });
    };

    // Transform grouped errors with click handlers
    const errorsBySource = {
      hashboard: transformErrorsForModal(groupedErrors.hashboard || [], deviceId, onClickHandler),
      psu: transformErrorsForModal(groupedErrors.psu || [], deviceId, onClickHandler),
      fan: transformErrorsForModal(groupedErrors.fan || [], deviceId, onClickHandler),
      controlBoard: transformErrorsForModal(groupedErrors.controlBoard || [], deviceId, onClickHandler),
      other: transformErrorsForModal(groupedErrors.other || [], deviceId, onClickHandler),
    };

    // Check if miner is sleeping (offline state in fleet context)
    // Don't show wake button if authentication is needed (can't trust INACTIVE/MAINTENANCE status)
    const isMinersleeping =
      (activeMiner?.deviceStatus === DeviceStatus.INACTIVE || activeMiner?.deviceStatus === DeviceStatus.MAINTENANCE) &&
      !needsAuthentication;

    // Build buttons
    const buttons = [];

    // Add wake miner button if miner is sleeping
    if (isMinersleeping) {
      buttons.push({
        text: "Wake miner",
        variant: variants.secondary,
        onClick: () => {
          handleClose();
          handleWakeMiner();
        },
      });
    }

    buttons.push({
      text: "Done",
      variant: variants.primary,
      onClick: handleClose,
    });

    return {
      props: {
        title: summary.title,
        subtitle: summary.subtitle,
        errors: errorsBySource,
        isSleeping: isMinersleeping,
        isOffline,
        needsAuthentication,
        needsMiningPool,
      },
      title: `${activeMiner?.name || deviceId} status`,
      buttons,
      onDismiss: handleClose,
    };
  }, [
    groupedErrors,
    summary,
    activeMiner,
    deviceId,
    handleWakeMiner,
    handleClose,
    isOffline,
    needsAuthentication,
    needsMiningPool,
  ]);

  // getComponentStatus function - returns complete data including config
  const getComponentStatus = useCallback(
    (address: ComponentAddress): ComponentStatusData | undefined => {
      const { componentType: type, componentId: id } = address;

      // Build component status props using the miner data and errors
      const props = buildComponentStatusProps(activeMiner, type, id, allErrors);

      if (!props) {
        // Return undefined if component not found
        return undefined;
      }

      const sharedType = mapErrorComponentTypeToShared(type);
      if (!sharedType) return undefined;

      return {
        props,
        title: getComponentTitle(sharedType),
        buttons: [
          {
            text: "Done",
            variant: variants.primary,
            onClick: handleClose,
          },
        ],
        onDismiss: handleClose,
        onNavigateBack: () => setComponent(undefined),
      };
    },
    [activeMiner, handleClose, allErrors],
  );

  // Don't render if no miner data
  if (!activeMiner) {
    return null;
  }

  // Render the shared StatusModal with integration data
  return (
    <SharedStatusModal
      open={isVisible}
      componentAddress={component}
      getMinerStatus={getMinerStatus}
      getComponentStatus={getComponentStatus}
      showBackButton={showBackButton}
    />
  );
};

export default ProtoFleetStatusModal;
