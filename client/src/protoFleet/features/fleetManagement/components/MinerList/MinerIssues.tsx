import { ReactNode, useMemo } from "react";
import { ComponentType as ErrorComponentType, type ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { transformFleetErrorsToShared } from "@/protoFleet/components/StatusModal/utils";
import { getComponentIcon } from "@/protoFleet/features/fleetManagement/components/MinerList/utils";
import { Alert } from "@/shared/assets/icons";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useMinerIssues } from "@/shared/hooks/useStatusSummary";

type MinerIssuesProps = {
  miner: MinerStateSnapshot;
  errors: ErrorMessage[];
  errorsLoaded: boolean;
  onClick?: () => void;
};

// Map from shared error keys to ErrorComponentType
const componentTypeMap: Record<string, ErrorComponentType> = {
  hashboard: ErrorComponentType.HASH_BOARD,
  psu: ErrorComponentType.PSU,
  fan: ErrorComponentType.FAN,
  controlBoard: ErrorComponentType.CONTROL_BOARD,
};

/** Group errors by component type (same logic as useGroupedErrors but pure) */
function groupErrors(errors: ErrorMessage[]) {
  const grouped = {
    hashboard: [] as ErrorMessage[],
    psu: [] as ErrorMessage[],
    fan: [] as ErrorMessage[],
    controlBoard: [] as ErrorMessage[],
    other: [] as ErrorMessage[],
  };
  errors.forEach((error) => {
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
}

const MinerIssues = ({ miner, errors, errorsLoaded, onClick }: MinerIssuesProps) => {
  const deviceStatus = miner.deviceStatus;

  // Group errors by component type
  const groupedErrors = useMemo(() => groupErrors(errors), [errors]);

  // Compute issue flags
  const needsAuthentication = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatus === DeviceStatus.NEEDS_MINING_POOL;
  const isUpdating = deviceStatus === DeviceStatus.UPDATING;
  const isRebootRequired = deviceStatus === DeviceStatus.REBOOT_REQUIRED;

  // Transform errors to shared format using existing utility
  const sharedErrors = useMemo(() => transformFleetErrorsToShared(groupedErrors), [groupedErrors]);

  // Compute issues summary (authentication, pool, firmware status, and hardware errors)
  const { summary, hasIssues } = useMinerIssues(
    needsAuthentication,
    needsMiningPool,
    sharedErrors,
    isUpdating,
    isRebootRequired,
  );

  // Determine icon to show based on issue type
  // Note: Auth and pool issues don't have icons (per Figma design)
  const icon = useMemo((): ReactNode | null => {
    // Auth and pool issues don't get icons
    if (needsAuthentication || needsMiningPool) {
      return null;
    }

    // Derive component types from sharedErrors
    const componentTypesWithErrors = Object.entries(sharedErrors)
      .filter(([, errors]) => errors.length > 0)
      .map(([key]) => componentTypeMap[key])
      .filter((type): type is ErrorComponentType => type !== undefined);

    if (componentTypesWithErrors.length === 0) return null;
    if (componentTypesWithErrors.length === 1) {
      return getComponentIcon(componentTypesWithErrors[0]);
    }
    return <Alert width="w-4" />;
  }, [needsAuthentication, needsMiningPool, sharedErrors]);

  // While errors haven't loaded, show shimmer for devices that could have issues
  if (!errorsLoaded && !needsAuthentication && !needsMiningPool && !isUpdating && !isRebootRequired) {
    return <SkeletonBar className="w-24" />;
  }

  // Show empty state if no issues
  if (!hasIssues) {
    return null;
  }

  // Issues should always be clickable (even for disabled rows)
  const isClickable = !!onClick;

  const content = (
    <>
      {icon}
      {summary}
    </>
  );

  return isClickable ? (
    <button type="button" className="flex cursor-pointer items-center gap-2 hover:underline" onClick={onClick}>
      {content}
    </button>
  ) : (
    <div className="flex items-center gap-2">{content}</div>
  );
};

export default MinerIssues;
