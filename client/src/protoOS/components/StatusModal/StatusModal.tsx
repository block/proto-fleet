import { useCallback, useState } from "react";
import { useComponentHardware, useComponentTelemetry } from "./hooks";
import type { ComponentAddress, ProtoOSStatusModalProps } from "./types";
import {
  buildComponentStatusProps,
  getComponentTitle,
  transformErrorsForModal,
} from "./utils";
import { useCoolingStatus } from "@/protoOS/api/hooks/useCoolingStatus";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { WakingDialog } from "@/protoOS/components/Power";
import { useMinerStatusTitle } from "@/protoOS/hooks/status";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { useErrors, useGroupedErrors, useIsSleeping } from "@/protoOS/store";
import type { ErrorSource } from "@/protoOS/store/types";
import { variants } from "@/shared/components/Button";
import { StatusModal as SharedStatusModal } from "@/shared/components/StatusModal";
import type {
  ComponentStatusData,
  MinerStatusData,
} from "@/shared/components/StatusModal/types";

/**
 * ProtoOS-specific StatusModal wrapper that integrates with the store
 *
 * This component encapsulates all the integration logic between the ProtoOS store
 * and the shared StatusModal component. It handles:
 * - Store data fetching and transformation
 * - Component navigation state
 * - Wake miner functionality
 * - Error grouping and formatting
 *
 * @example
 * ```tsx
 * const [isModalOpen, setModalOpen] = useState(false);
 *
 * <ProtoOSStatusModal
 *   show={isModalOpen}
 *   onClose={() => setModalOpen(false)}
 * />
 * ```
 */
const ProtoOSStatusModal = ({
  show,
  onClose,
  componentAddress,
  showBackButton = true,
}: ProtoOSStatusModalProps) => {
  // Component navigation state
  const [component, setComponent] = useState<ComponentAddress | undefined>(
    componentAddress,
  );

  // ProtoOS store hooks
  const errors = useErrors();
  const groupedErrors = useGroupedErrors();
  const { title, subtitle } = useMinerStatusTitle();
  const isSleeping = useIsSleeping();

  // Wake functionality
  const { wakeMiner, shouldWake } = useWakeMiner();

  // Fetch all telemetry data when modal is open
  // This ensures data is immediately available when navigating to any component
  useTelemetry({
    level: ["miner", "hashboard", "psu"],
    poll: true,
    pollIntervalMs: 15 * 1000,
  });

  // Also fetch cooling status for fan data
  useCoolingStatus({
    poll: true,
  });

  // Stabilize component values to prevent unnecessary re-renders
  const componentSource = component?.source || "SYSTEM";
  const componentIndex = component?.componentIndex;

  // Fetch telemetry and hardware data for the selected component
  const componentTelemetry = useComponentTelemetry(
    componentSource,
    componentIndex,
  );
  const componentHardware = useComponentHardware(
    componentSource,
    componentIndex,
  );

  // getMinerStatus function - returns complete data including config
  const getMinerStatus = useCallback((): MinerStatusData => {
    // Create onClick handler that navigates to component details
    const onClickHandler = (source: ErrorSource, componentIndex?: number) => {
      setComponent({ source, componentIndex });
    };

    // Transform grouped errors with click handlers (using hook's grouping)
    const errorsBySource = {
      hashboard: transformErrorsForModal(
        groupedErrors.hashboard || [],
        onClickHandler,
      ),
      psu: transformErrorsForModal(groupedErrors.psu || [], onClickHandler),
      fan: transformErrorsForModal(groupedErrors.fan || [], onClickHandler),
      controlBoard: transformErrorsForModal(
        groupedErrors.system || [],
        onClickHandler,
      ),
    };

    // Build buttons
    const buttons = [];
    if (isSleeping) {
      buttons.push({
        text: "Wake miner",
        variant: variants.secondary,
        onClick: () => {
          onClose();
          wakeMiner();
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
        isSleeping,
      },
      title: "Miner status",
      buttons,
      onDismiss: onClose,
    };
  }, [groupedErrors, title, subtitle, isSleeping, onClose, wakeMiner]);

  // getComponentStatus function - returns complete data including config
  const getComponentStatus = useCallback(
    (address: ComponentAddress): ComponentStatusData | undefined => {
      const { source, componentIndex } = address;

      // Use the telemetry and hardware data from hooks
      // Note: These hooks will be updated when the component changes
      const props = buildComponentStatusProps(
        source,
        componentIndex,
        errors,
        componentTelemetry,
        componentHardware,
      );

      if (!props) {
        // Return undefined if component not found
        return undefined;
      }

      return {
        props,
        title: getComponentTitle(props.componentType),
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
    [errors, componentTelemetry, componentHardware, onClose],
  );

  // If miner is waking, show the waking dialog instead
  if (shouldWake) {
    return <WakingDialog show={shouldWake} />;
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

export default ProtoOSStatusModal;
