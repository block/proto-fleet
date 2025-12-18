import { ReactNode, useMemo } from "react";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { SUPPORTED_COMPONENT_TYPES } from "@/protoFleet/components/StatusModal/constants";
import { useFleetStore, useMiner, useMinerDeviceStatus } from "@/protoFleet/store";
import { Alert, ControlBoard, Fan, Hashboard, LightningAlt } from "@/shared/assets/icons";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";
import type { GroupedStatusErrors } from "@/shared/hooks/useStatusSummary";
import { useMinerStatusSummary } from "@/shared/hooks/useStatusSummary";

type MinerStatusProps = {
  deviceIdentifier: string;
  selectedItems?: string[];
  onClick?: () => void;
};

// Map error component type to icon
function getComponentIcon(componentType: ErrorComponentType): ReactNode {
  switch (componentType) {
    case ErrorComponentType.CONTROL_BOARD:
      return <ControlBoard width="w-4" />;
    case ErrorComponentType.HASH_BOARD:
      return <Hashboard width="w-4" />;
    case ErrorComponentType.FAN:
      return <Fan width="w-4" />;
    case ErrorComponentType.PSU:
      return <LightningAlt width="w-4" />;
    default:
      return <Alert width="w-4" />;
  }
}

const MinerStatus = ({ deviceIdentifier, onClick }: MinerStatusProps) => {
  const miner = useMiner(deviceIdentifier);
  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");

  // Get errors from normalized store
  const selectErrorsByDevice = useFleetStore((state) => state.fleet.selectErrorsByDevice);
  const errors = selectErrorsByDevice(deviceIdentifier);

  // Compute status flags
  const isSleeping = deviceStatusFromStore === DeviceStatus.INACTIVE;
  const isOffline = deviceStatusFromStore === DeviceStatus.OFFLINE;
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;

  // Transform errors to shared format
  const sharedErrors = useMemo((): GroupedStatusErrors => {
    const result: GroupedStatusErrors = {
      hashboard: [],
      psu: [],
      fan: [],
      controlBoard: [],
    };

    if (!errors || errors.length === 0) return result;

    errors.forEach((error) => {
      if (!SUPPORTED_COMPONENT_TYPES.has(error.componentType)) return;

      const parsed = error.componentId ? parseInt(error.componentId, 10) : NaN;
      const componentIndex = !isNaN(parsed) ? parsed : undefined;

      switch (error.componentType) {
        case ErrorComponentType.HASH_BOARD:
          result.hashboard.push({ componentType: "hashboard", componentIndex });
          break;
        case ErrorComponentType.PSU:
          result.psu.push({ componentType: "psu", componentIndex });
          break;
        case ErrorComponentType.FAN:
          result.fan.push({ componentType: "fan", componentIndex });
          break;
        case ErrorComponentType.CONTROL_BOARD:
          result.controlBoard.push({ componentType: "controlBoard", componentIndex });
          break;
      }
    });

    return result;
  }, [errors]);

  // Use shared hook for condensed text
  const summary = useMinerStatusSummary(sharedErrors, isSleeping, isOffline, needsAuthentication);

  // Determine icon based on error component types (UI-specific)
  // Don't show error icon when miner is in an inactive state (sleeping/offline/needs auth)
  const errorIcon = useMemo((): ReactNode | null => {
    if (isOffline || isSleeping || needsAuthentication) {
      return null;
    }
    if (!errors || errors.length === 0) return null;

    const componentTypes = new Set<ErrorComponentType>();
    errors.forEach((error) => {
      if (SUPPORTED_COMPONENT_TYPES.has(error.componentType)) {
        componentTypes.add(error.componentType);
      }
    });

    if (componentTypes.size === 0) return null;
    if (componentTypes.size === 1) {
      return getComponentIcon(Array.from(componentTypes)[0]);
    }
    return <Alert width="w-4" />;
  }, [errors, isOffline, isSleeping, needsAuthentication]);

  // Determine StatusCircle status based on flags and errors
  const circleStatus = useMemo(() => {
    if (isOffline || isSleeping || needsAuthentication) {
      return statuses.inactive;
    }
    if (errorIcon) {
      return statuses.error;
    }
    return statuses.normal;
  }, [isOffline, isSleeping, needsAuthentication, errorIcon]);

  // Determine if the status should be clickable
  const isClickable = onClick && (!needsAuthentication || isSleeping);

  return (
    <div
      className={`flex items-center gap-2 ${isClickable ? "cursor-pointer hover:underline" : ""}`}
      onClick={isClickable ? onClick : undefined}
    >
      <StatusCircle status={circleStatus} variant="simple" width="w-[6px]" />
      {errorIcon}
      {summary.condensed}
    </div>
  );
};

export default MinerStatus;
