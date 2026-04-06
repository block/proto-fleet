import { type MouseEvent, ReactNode, useMemo } from "react";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useGroupedErrors } from "@/protoFleet/components/StatusModal/hooks/useStatusModalHooks";
import { transformFleetErrorsToShared } from "@/protoFleet/components/StatusModal/utils";
import { getComponentIcon } from "@/protoFleet/features/fleetManagement/components/MinerList/utils";
import { useFleetStore, useMiner, useMinerDeviceStatus } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useMinerIssues } from "@/shared/hooks/useStatusSummary";

type MinerIssuesProps = {
  deviceIdentifier: string;
  onClick?: () => void;
};

// Map from shared error keys to ErrorComponentType
const componentTypeMap: Record<string, ErrorComponentType> = {
  hashboard: ErrorComponentType.HASH_BOARD,
  psu: ErrorComponentType.PSU,
  fan: ErrorComponentType.FAN,
  controlBoard: ErrorComponentType.CONTROL_BOARD,
};

const MinerIssues = ({ deviceIdentifier, onClick }: MinerIssuesProps) => {
  const miner = useMiner(deviceIdentifier);
  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");
  const errorsLoaded = useFleetStore((state) => state.fleet.errors.metadata.lastFetchedAt !== null);

  // Get errors from normalized store using existing hook
  const groupedErrors = useGroupedErrors(deviceIdentifier);

  // Compute issue flags
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatusFromStore === DeviceStatus.NEEDS_MINING_POOL;
  const isUpdating = deviceStatusFromStore === DeviceStatus.UPDATING;
  const isRebootRequired = deviceStatusFromStore === DeviceStatus.REBOOT_REQUIRED;

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

  return (
    <div
      className={`flex items-center gap-2 ${isClickable ? "cursor-pointer hover:underline" : ""}`}
      onClick={
        isClickable
          ? (e: MouseEvent) => {
              e.stopPropagation();
              onClick?.();
            }
          : undefined
      }
    >
      {icon}
      {summary}
    </div>
  );
};

export default MinerIssues;
