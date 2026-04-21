import { ReactNode, useMemo } from "react";
import { statusColumnLoadingMessages } from "../MinerActionsMenu/constants";
import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { DeviceStatus, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { BatchOperation } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";
import { isActionLoading } from "@/protoFleet/features/fleetManagement/utils/batchStatusCheck";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SkeletonBar from "@/shared/components/SkeletonBar";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";
import { useNeedsAttention } from "@/shared/hooks/useNeedsAttention";
import { useMinerStatus } from "@/shared/hooks/useStatusSummary";

type StatusWrapperProps = {
  onClick?: () => void;
  children: ReactNode;
};

const StatusWrapper = ({ onClick, children }: StatusWrapperProps) => {
  if (onClick) {
    return (
      <button type="button" className="flex cursor-pointer items-center gap-2 hover:underline" onClick={onClick}>
        {children}
      </button>
    );
  }
  return <div className="flex items-center gap-2">{children}</div>;
};

type MinerStatusProps = {
  miner: MinerStateSnapshot;
  errors: ErrorMessage[];
  activeBatches: BatchOperation[];
  errorsLoaded: boolean;
  onClick?: () => void;
};

const MinerStatus = ({ miner, errors, activeBatches, errorsLoaded, onClick }: MinerStatusProps) => {
  const deviceStatusFromStore = miner.deviceStatus;

  // Compute status flags
  const needsAuthentication = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const isPaired = miner.pairingStatus === PairingStatus.PAIRED;
  // Paired miners with UNSPECIFIED device_status (typically freshly paired, not yet polled)
  // are treated as offline — this matches the Fleet Health dashboard and Offline filter.
  const isOffline =
    deviceStatusFromStore === DeviceStatus.OFFLINE || (deviceStatusFromStore === DeviceStatus.UNSPECIFIED && isPaired);
  // When authentication is needed, we can't trust INACTIVE/MAINTENANCE status
  // (could be sleeping OR showing as inactive because we can't authenticate)
  const isSleeping =
    (deviceStatusFromStore === DeviceStatus.INACTIVE || deviceStatusFromStore === DeviceStatus.MAINTENANCE) &&
    !needsAuthentication;
  const needsMiningPool = deviceStatusFromStore === DeviceStatus.NEEDS_MINING_POOL;
  const hasDeviceError = deviceStatusFromStore === DeviceStatus.ERROR;
  const isUpdating = deviceStatusFromStore === DeviceStatus.UPDATING;
  const isRebootRequired = deviceStatusFromStore === DeviceStatus.REBOOT_REQUIRED;

  const needsAttention = useNeedsAttention(
    needsAuthentication,
    needsMiningPool,
    errors,
    hasDeviceError,
    isUpdating || isRebootRequired,
  );

  // Compute status (Hashing, Offline, Sleeping, or Needs attention)
  const status = useMinerStatus(isOffline, isSleeping, needsAttention);

  // Determine StatusCircle visual indicator based on flags
  // Priority: (offline | sleeping) > needs attention > normal
  // Note: isSleeping is already filtered to exclude auth-needed devices
  const circleStatus = useMemo(() => {
    if (isOffline || isSleeping) {
      return statuses.sleeping;
    }
    if (needsAttention) {
      return statuses.error;
    }
    return statuses.normal;
  }, [isOffline, isSleeping, needsAttention]);

  // Check for active batch operations FIRST (highest priority)
  const activeBatch = activeBatches[0];
  const batchLoadingMessage = activeBatch ? statusColumnLoadingMessages[activeBatch.action] : null;

  if (isActionLoading(activeBatch, deviceStatusFromStore)) {
    const content = (
      <>
        <StatusCircle status={statuses.pending} variant="simple" width="w-[6px]" testId="miner-status-indicator" />
        <ProgressCircular size={14} indeterminate />
        <span className="text-text-primary-50">{batchLoadingMessage}</span>
      </>
    );

    return <StatusWrapper onClick={onClick}>{content}</StatusWrapper>;
  }

  // Firmware update states — show dedicated indicators
  if (isUpdating) {
    return (
      <StatusWrapper onClick={onClick}>
        <StatusCircle status={statuses.error} variant="simple" width="w-[6px]" testId="miner-status-indicator" />
        <ProgressCircular size={14} indeterminate />
        Updating firmware
      </StatusWrapper>
    );
  }

  if (isRebootRequired) {
    return (
      <StatusWrapper onClick={onClick}>
        <StatusCircle status={statuses.error} variant="simple" width="w-[6px]" testId="miner-status-indicator" />
        Reboot required
      </StatusWrapper>
    );
  }

  // While errors haven't loaded yet, devices that would default to "Hashing"
  // might actually need attention once errors arrive — show shimmer instead
  if (!errorsLoaded && status === "Hashing") {
    return <SkeletonBar className="w-20" />;
  }

  return (
    <StatusWrapper onClick={onClick}>
      <StatusCircle status={circleStatus} variant="simple" width="w-[6px]" testId="miner-status-indicator" />
      {status}
    </StatusWrapper>
  );
};

export default MinerStatus;
