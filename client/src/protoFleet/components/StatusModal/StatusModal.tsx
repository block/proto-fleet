import { useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { SUPPORTED_COMPONENT_TYPES } from "./constants";
import { useDeviceErrors, useGroupedErrors, useMinerData } from "./hooks";
import type { ComponentAddress, ProtoFleetStatusModalProps } from "./types";
import {
  buildComponentStatusProps,
  getComponentTitle,
  getSummaryText,
  mapErrorComponentTypeToShared,
  transformErrorsForModal,
} from "./utils";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { StartMiningRequestSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useComponentErrorDetail } from "@/protoFleet/api/useComponentErrors";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { variants } from "@/shared/components/Button";
import { StatusModal as SharedStatusModal } from "@/shared/components/StatusModal";
import type { ComponentStatusData, MinerStatusData } from "@/shared/components/StatusModal/types";
import { pushToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";

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
  const errorStatus = useDeviceErrors(deviceId);
  const groupedErrors = useGroupedErrors(deviceId);

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

  // Check if there are any supported errors
  const hasSupportedErrors = useMemo(() => {
    return (
      errorStatus?.errors?.some((error) => {
        // Use componentType directly from error
        return error.componentType && SUPPORTED_COMPONENT_TYPES.has(error.componentType);
      }) || false
    );
  }, [errorStatus]);

  // TODO: Find a design solution for displaying unsupported error types (EEPROM, IO_MODULE)
  // Currently we hide them and show "All systems operational" but we may want to
  // indicate there are issues that aren't yet displayed in the UI

  // Get title and subtitle - use "All systems operational" if no supported errors
  const { title, subtitle } = getSummaryText(hasSupportedErrors ? errorStatus?.summary : undefined);

  // Stabilize component values to prevent unnecessary re-renders
  const componentType = component?.componentType;
  const componentId = component?.componentId;

  // Check if the selected component has errors
  const componentHasErrors = useMemo(() => {
    if (!component || !miner?.errorStatus?.errors) return false;

    // Check if any errors match this component by type and ID
    return miner.errorStatus.errors.some((error) => {
      return error.componentType === componentType && error.componentId === componentId;
    });
  }, [component, miner, componentType, componentId]);

  // Fetch component error detail when component is selected and has errors
  const { summary: componentSummary } = useComponentErrorDetail(
    deviceId,
    componentId, // Use component ID for API calls
    Boolean(component && componentHasErrors), // Only fetch when component is selected and has errors
  );

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
        title,
        subtitle,
        errors: errorsBySource,
        isSleeping: isMinersleeping,
      },
      title: `${miner?.name || deviceId} Status`,
      buttons,
      onDismiss: onClose,
    };
  }, [groupedErrors, title, subtitle, miner, deviceId, onClose, handleWakeMiner]);

  // getComponentStatus function - returns complete data including config
  const getComponentStatus = useCallback(
    (address: ComponentAddress): ComponentStatusData | undefined => {
      const { componentType: type, componentId: id } = address;

      // Build component status props using the miner data and API summary
      const props = buildComponentStatusProps(miner, type, id, componentSummary);

      if (!props) {
        // Return undefined if component not found
        return undefined;
      }

      const sharedType = mapErrorComponentTypeToShared(type);
      if (!sharedType) return undefined;

      // Don't override with loading state - let the props from buildComponentStatusProps stand
      // If componentSummary is undefined, title/subtitle will be empty strings

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
    [miner, onClose, componentSummary],
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
