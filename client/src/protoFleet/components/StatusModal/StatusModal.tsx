import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { ComponentAddress, ProtoFleetStatusModalProps } from "./types";
import {
  buildComponentStatusProps,
  getComponentTitle,
  mapErrorComponentTypeToShared,
  transformErrorsForModal,
  transformFleetErrorsToShared,
} from "./utils";
import { ComponentType as ErrorComponentType, type ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { StartMiningRequestSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useDeviceErrors } from "@/protoFleet/api/useDeviceErrors";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
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
}: ProtoFleetStatusModalProps) => {
  const isVisible = open ?? true;

  // Component navigation state
  const [component, setComponent] = useState<ComponentAddress | undefined>(componentAddress);

  // Fetch errors for this device when modal is visible
  const modalDeviceIds = useMemo(() => (isVisible && deviceId ? [deviceId] : EMPTY_DEVICE_IDS), [isVisible, deviceId]);
  const { errorsByDevice } = useDeviceErrors(modalDeviceIds);

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
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const isOffline = miner?.deviceStatus === DeviceStatus.OFFLINE;
  // When authentication is needed, we can't trust INACTIVE (or MAINTENANCE) status
  // (could be sleeping OR showing as inactive/maintenance because we can't authenticate)
  const isSleeping =
    (miner?.deviceStatus === DeviceStatus.INACTIVE || miner?.deviceStatus === DeviceStatus.MAINTENANCE) &&
    !needsAuthentication;
  const needsMiningPool = miner?.deviceStatus === DeviceStatus.NEEDS_MINING_POOL;

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
      (miner?.deviceStatus === DeviceStatus.INACTIVE || miner?.deviceStatus === DeviceStatus.MAINTENANCE) &&
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
      title: `${miner?.name || deviceId} status`,
      buttons,
      onDismiss: handleClose,
    };
  }, [
    groupedErrors,
    summary,
    miner,
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
      const props = buildComponentStatusProps(miner, type, id, allErrors);

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
    [miner, handleClose, allErrors],
  );

  // Don't render if no miner data
  if (!miner) {
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
