import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { useGroupedErrors, useMinerData } from "./hooks";
import type { ComponentAddress, ProtoFleetStatusModalProps } from "./types";
import {
  buildComponentStatusProps,
  getComponentTitle,
  mapErrorComponentTypeToShared,
  transformErrorsForModal,
  transformFleetErrorsToShared,
} from "./utils";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { StartMiningRequestSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { useFleetStore } from "@/protoFleet/store";
import { variants } from "@/shared/components/Button";
import { StatusModal as SharedStatusModal } from "@/shared/components/StatusModal";
import type { ComponentStatusData, MinerStatusData } from "@/shared/components/StatusModal/types";
import { pushToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { useMinerStatusSummary } from "@/shared/hooks/useStatusSummary";

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
 *   show={isModalOpen}
 *   onClose={() => setModalOpen(false)}
 *   deviceId={minerId}
 * />
 * ```
 */
const ProtoFleetStatusModal = ({
  show,
  onClose,
  deviceId,
  componentAddress,
  showBackButton = true,
}: ProtoFleetStatusModalProps) => {
  // Component navigation state
  const [component, setComponent] = useState<ComponentAddress | undefined>(componentAddress);

  // ProtoFleet store hooks
  const miner = useMinerData(deviceId);
  const groupedErrors = useGroupedErrors(deviceId);

  // Get errors from normalized store for component status
  const selectErrorsByDevice = useFleetStore((state) => state.fleet.selectErrorsByDevice);
  const allErrors = selectErrorsByDevice(deviceId);

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
  const isSleeping = miner?.deviceStatus === DeviceStatus.INACTIVE;
  const isOffline = miner?.deviceStatus === DeviceStatus.OFFLINE;
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
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
    };

    // Check if miner is sleeping (offline state in fleet context)
    const isMinersleeping = miner?.deviceStatus === DeviceStatus.INACTIVE;

    // Build buttons
    const buttons = [];

    // Add wake miner button if miner is sleeping
    if (isMinersleeping) {
      buttons.push({
        text: "Wake miner",
        variant: variants.secondary,
        onClick: () => {
          onClose();
          handleWakeMiner();
        },
      });
    }

    buttons.push({
      text: "Done",
      variant: variants.primary,
      onClick: onClose,
    });

    return {
      props: {
        title: summary.title,
        subtitle: summary.subtitle,
        errors: errorsBySource,
        isSleeping: isMinersleeping,
        isOffline,
      },
      title: `${miner?.name || deviceId} Status`,
      buttons,
      onDismiss: onClose,
    };
  }, [groupedErrors, summary, miner, deviceId, onClose, handleWakeMiner, isOffline]);

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
            onClick: onClose,
          },
        ],
        onDismiss: onClose,
        onNavigateBack: () => setComponent(undefined),
      };
    },
    [miner, onClose, allErrors],
  );

  // Don't render if no miner data
  if (!miner) {
    return null;
  }

  // Render the shared StatusModal with integration data
  return (
    <SharedStatusModal
      componentAddress={component}
      getMinerStatus={getMinerStatus}
      getComponentStatus={getComponentStatus}
      show={show}
      showBackButton={showBackButton}
    />
  );
};

export default ProtoFleetStatusModal;
