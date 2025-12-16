import { ReactNode, useMemo } from "react";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { SUPPORTED_COMPONENT_TYPES } from "@/protoFleet/components/StatusModal/constants";
import { useMiner, useMinerDeviceStatus } from "@/protoFleet/store";
import { Alert, ControlBoard, Fan, Hashboard, LightningAlt } from "@/shared/assets/icons";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";

type MinerStatusProps = {
  deviceIdentifier: string;
  selectedItems?: string[];
  onClick?: () => void;
};

// Get icon for a specific error component type
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

// Get display text for a component type
function getComponentDisplayText(componentType: ErrorComponentType, statusType: "error" | "warning"): string {
  const suffix = statusType === "error" ? "Failure" : "Warning";
  switch (componentType) {
    case ErrorComponentType.CONTROL_BOARD:
      return `Control Board ${suffix}`;
    case ErrorComponentType.HASH_BOARD:
      return `Hashboard ${suffix}`;
    case ErrorComponentType.FAN:
      return `Fan ${suffix}`;
    case ErrorComponentType.PSU:
      return `Power ${suffix}`;
    default:
      return statusType === "error" ? "Device Error" : "Device Warning";
  }
}

const MinerStatus = ({ deviceIdentifier, onClick }: MinerStatusProps) => {
  const miner = useMiner(deviceIdentifier);
  const authenticationNeeded = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");
  const errorStatus = miner?.errorStatus;

  const status = useMemo(() => {
    // Priority 1: Device inactive (Sleeping)
    if (deviceStatusFromStore === DeviceStatus.INACTIVE) {
      return (
        <>
          <StatusCircle status={statuses.inactive} variant="simple" width="w-[6px]" />
          Sleeping
        </>
      );
    }

    // Priority 2: Device offline
    if (deviceStatusFromStore === DeviceStatus.OFFLINE) {
      return (
        <>
          <StatusCircle status={statuses.inactive} variant="simple" width="w-[6px]" />
          Offline
        </>
      );
    }

    // Priority 3: Authentication needed
    if (authenticationNeeded) {
      return (
        <>
          <StatusCircle status={statuses.inactive} variant="simple" width="w-[6px]" />
          Needs Authentication
        </>
      );
    }

    // Priority 4: Error status from errors API (only for supported component types)
    if (errorStatus && errorStatus.errors && errorStatus.errors.length > 0) {
      // Analyze component types from errors, filtering for supported types only
      const componentTypes = new Set<ErrorComponentType>();
      let hasSupportedErrors = false;

      errorStatus.errors.forEach((error) => {
        // Use componentType directly from error
        if (error.componentType && SUPPORTED_COMPONENT_TYPES.has(error.componentType)) {
          componentTypes.add(error.componentType);
          hasSupportedErrors = true;
        }
      });

      // Only show error status if we have supported component errors
      if (hasSupportedErrors) {
        // Determine icon based on component types
        let icon: ReactNode;
        let displayText: string;

        if (componentTypes.size === 1) {
          // All errors are from the same component type - show specific icon
          const componentType = Array.from(componentTypes)[0];
          icon = getComponentIcon(componentType);

          // Use condensed summary if available, otherwise use component-specific text
          displayText = errorStatus.summary?.condensed || getComponentDisplayText(componentType, "error");
        } else if (componentTypes.size > 1) {
          // Multiple component types - show generic alert icon
          icon = <Alert width="w-4" />;
          displayText = errorStatus.summary?.condensed || "Device Error";
        } else {
          // No supported component types specified in errors
          icon = <Alert width="w-4" />;
          displayText = errorStatus.summary?.condensed || "Device Error";
        }

        return (
          <>
            <StatusCircle status={statuses.error} variant="simple" width="w-[6px]" />
            {icon}
            {displayText}
          </>
        );
      }
      // If we only have unsupported errors (EEPROM, IO_MODULE), fall through to show "Hashing"
    }

    // Note: Component status is now exclusively tracked via the errors API above.
    // The old ComponentStatus/MinerComponentStatus types have been removed from the proto definitions.

    return (
      <>
        <StatusCircle status={statuses.normal} variant="simple" width="w-[6px]" />
        Hashing
      </>
    );
  }, [authenticationNeeded, deviceStatusFromStore, errorStatus]);

  // Determine if the status should be clickable
  // Clickable: All states except "Needs Authentication" (which has a different flow)
  // This includes: Hashing, Error states, Warning states, Sleeping, Offline
  // Special case: Sleeping status is always clickable even if authentication is needed
  const isSleeping = deviceStatusFromStore === DeviceStatus.INACTIVE;
  const isClickable = onClick && (!authenticationNeeded || isSleeping);

  return (
    <div
      className={`flex items-center gap-2 ${isClickable ? "cursor-pointer hover:underline" : ""}`}
      onClick={isClickable ? onClick : undefined}
    >
      {status}
    </div>
  );
};

export default MinerStatus;
