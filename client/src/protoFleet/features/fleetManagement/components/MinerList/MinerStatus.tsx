import { useMemo } from "react";
import { statusColumnLoadingMessages } from "../MinerActionsMenu/constants";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { hasReachedExpectedStatus } from "@/protoFleet/features/fleetManagement/utils/batchStatusCheck";
import { useFleetStore, useMiner, useMinerActiveBatches, useMinerDeviceStatus } from "@/protoFleet/store";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { statuses } from "@/shared/components/StatusCircle";
import { useNeedsAttention } from "@/shared/hooks/useNeedsAttention";
import { useMinerStatus } from "@/shared/hooks/useStatusSummary";

type MinerStatusProps = {
  deviceIdentifier: string;
  selectedItems?: string[];
  onClick?: () => void;
};

const MinerStatus = ({ deviceIdentifier, onClick }: MinerStatusProps) => {
  const miner = useMiner(deviceIdentifier);
  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");
  const activeBatches = useMinerActiveBatches(deviceIdentifier);

  // Get errors from normalized store
  const selectErrorsByDevice = useFleetStore((state) => state.fleet.selectErrorsByDevice);
  const errors = selectErrorsByDevice(deviceIdentifier);

  // Compute status flags
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const isOffline = deviceStatusFromStore === DeviceStatus.OFFLINE;
  // When authentication is needed, we can't trust INACTIVE/MAINTENANCE status
  // (could be sleeping OR showing as inactive because we can't authenticate)
  const isSleeping =
    (deviceStatusFromStore === DeviceStatus.INACTIVE || deviceStatusFromStore === DeviceStatus.MAINTENANCE) &&
    !needsAuthentication;
  const needsMiningPool = deviceStatusFromStore === DeviceStatus.NEEDS_MINING_POOL;

  const needsAttention = useNeedsAttention(needsAuthentication, needsMiningPool, errors);

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

  // Status should always be clickable (even for disabled rows)
  const isClickable = !!onClick;

  // Check for active batch operations FIRST (highest priority)
  const hasActiveBatch = activeBatches.length > 0;
  const batchAction = hasActiveBatch ? activeBatches[0].action : null;
  const batchStartedAt = hasActiveBatch ? activeBatches[0].startedAt : undefined;
  const batchLoadingMessage = batchAction ? statusColumnLoadingMessages[batchAction] : null;

  // Check if device has reached expected status for this batch action
  const deviceHasReachedExpectedStatus = useMemo(() => {
    if (!batchAction) return false;
    return hasReachedExpectedStatus(batchAction, deviceStatusFromStore, batchStartedAt);
  }, [batchAction, deviceStatusFromStore, batchStartedAt]);

  // Show loading state only if batch is active AND device hasn't reached expected status yet
  if (hasActiveBatch && batchLoadingMessage && !deviceHasReachedExpectedStatus) {
    return (
      <div
        className={`flex items-center gap-2 ${isClickable ? "cursor-pointer hover:underline" : ""}`}
        onClick={isClickable ? onClick : undefined}
      >
        <StatusCircle status={circleStatus} variant="simple" width="w-[6px]" />
        <ProgressCircular size={14} indeterminate />
        <span className="text-text-primary-50">{batchLoadingMessage}</span>
      </div>
    );
  }

  return (
    <div
      className={`flex items-center gap-2 ${isClickable ? "cursor-pointer hover:underline" : ""}`}
      onClick={isClickable ? onClick : undefined}
    >
      <StatusCircle status={circleStatus} variant="simple" width="w-[6px]" />
      {status}
    </div>
  );
};

export default MinerStatus;
