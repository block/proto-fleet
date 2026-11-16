import { useCallback, useMemo, useState } from "react";
import ContainerHealthModal from "./ContainerHealthModal";
import { useComponentHardware, useComponentTelemetry } from "./hooks";
import type { ComponentAddress, ProtoOSStatusModalProps } from "./types";
import { buildComponentStatusProps, getComponentTitle, transformErrorsForModal } from "./utils";
import { useCoolingStatus } from "@/protoOS/api/hooks/useCoolingStatus";
import { useErrors as useRefreshErrors } from "@/protoOS/api/hooks/useErrors";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import { WakingDialog } from "@/protoOS/components/Power";
import { useMinerStatusTitle } from "@/protoOS/hooks/status";
import { useWakeMiner } from "@/protoOS/hooks/useWakeMiner";
import { useDeviceType, useIsSleeping, useMinerStore } from "@/protoOS/store";
import type { ComponentRef } from "@/protoOS/store/contract";
import { isPlantAlarmError, useDetailedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import { componentName, resolveComponentAddress } from "@/protoOS/store/utils/componentSelection";
import { variants } from "@/shared/components/Button";
import { StatusModal as SharedStatusModal } from "@/shared/components/StatusModal";
import type { ComponentStatusData, MinerStatusData } from "@/shared/components/StatusModal/types";

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
 *   open={isModalOpen}
 *   onClose={() => setModalOpen(false)}
 * />
 * ```
 */
const RigStatusModal = ({ open, onClose, componentAddress, showBackButton = true }: ProtoOSStatusModalProps) => {
  const isVisible = open ?? true;
  // Component navigation state
  const [component, setComponent] = useState<ComponentAddress | undefined>(
    () => resolveComponentAddress(componentAddress, useMinerStore.getState().hardware) ?? componentAddress,
  );

  // ProtoOS store hooks
  const errors = useDetailedErrors();
  useRefreshErrors({ enabled: isVisible, poll: isVisible, pollIntervalMs: 15_000 });
  const hardware = useMinerStore((state) => state.hardware);
  const selected = useMemo(() => resolveComponentAddress(component, hardware), [component, hardware]);
  // Pin a late-arriving rig slot to its resolved ID before rendering details.
  if (selected && component && !("kind" in component)) setComponent(selected);
  const name = useCallback((ref: ComponentRef) => componentName(ref, hardware), [hardware]);
  const { title, subtitle } = useMinerStatusTitle();
  const isSleeping = useIsSleeping();

  // Wake functionality
  const { wakeMiner, shouldWake } = useWakeMiner();

  // Fetch all telemetry data when modal is open
  // This ensures data is immediately available when navigating to any component
  useTelemetry({
    level: ["miner", "hashboard", "psu"],
    enabled: isVisible,
    poll: isVisible,
    pollIntervalMs: 15 * 1000,
  });

  // Also fetch cooling status for fan data
  useCoolingStatus({
    enabled: isVisible,
    poll: isVisible,
  });

  const handleClose = useCallback(() => {
    setComponent(resolveComponentAddress(componentAddress, useMinerStore.getState().hardware) ?? componentAddress);
    onClose();
  }, [componentAddress, onClose]);

  const componentTelemetry = useComponentTelemetry(selected);
  const componentHardware = useComponentHardware(selected);

  // getMinerStatus function - returns complete data including config
  const getMinerStatus = useCallback((): MinerStatusData => {
    const group = (kind: ComponentRef["kind"]) =>
      transformErrorsForModal(
        errors.filter((error) => error.component?.kind === kind),
        name,
        setComponent,
      );
    const errorsBySource = {
      hashboard: group("hashboard"),
      psu: group("psu"),
      fan: group("fan"),
      controlBoard: transformErrorsForModal(
        errors.filter(
          (error) => error.component?.kind === "controlBoard" || (!error.component && !isPlantAlarmError(error)),
        ),
        name,
        setComponent,
      ),
      other: transformErrorsForModal(errors.filter(isPlantAlarmError), name),
    };

    // Build buttons
    const buttons = [];
    if (isSleeping) {
      buttons.push({
        text: "Wake miner",
        variant: variants.secondary,
        onClick: () => {
          handleClose();
          wakeMiner();
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
        title,
        subtitle,
        errors: errorsBySource,
        isSleeping,
      },
      title: "Miner status",
      buttons,
      onDismiss: handleClose,
    };
  }, [errors, name, title, subtitle, isSleeping, handleClose, wakeMiner]);

  // getComponentStatus function - returns complete data including config
  const getComponentStatus = useCallback(
    (address: ComponentAddress): ComponentStatusData | undefined => {
      const ref = resolveComponentAddress(address, hardware);
      if (!ref)
        return {
          props: { componentType: "other", summary: "Component unavailable", errors: [] },
          title: "Component status",
          buttons: [],
          onDismiss: handleClose,
        };

      // Use the telemetry and hardware data from hooks
      // Note: These hooks will be updated when the component changes
      const props = buildComponentStatusProps(ref, name(ref), errors, componentTelemetry, componentHardware);

      return {
        props,
        title: `${getComponentTitle(props.componentType)} · ${name(ref)}`,
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
    [errors, hardware, name, componentTelemetry, componentHardware, handleClose],
  );

  // If miner is waking, show the waking dialog instead
  if (shouldWake) {
    return <WakingDialog open={shouldWake} />;
  }

  // Render the shared StatusModal with integration data
  return (
    <SharedStatusModal
      manageFocus
      componentAddress={component}
      getMinerStatus={getMinerStatus}
      getComponentStatus={getComponentStatus}
      open={isVisible}
      showBackButton={showBackButton}
    />
  );
};

const ProtoOSStatusModal = (props: ProtoOSStatusModalProps) => {
  const isContainer = useDeviceType() === "container";
  if (!isContainer) return <RigStatusModal {...props} />;
  // Remount each entry point so a previously selected component cannot leak
  // into another scope or a later opening of the same modal.
  const scopeKey = props.scope && props.scope.kind !== "container" ? props.scope.id : "container";
  return <ContainerHealthModal key={`${scopeKey}:${JSON.stringify(props.componentAddress)}`} {...props} />;
};

export default ProtoOSStatusModal;
