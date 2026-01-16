import { useMemo } from "react";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useFleetStore, useMiner, useMinerDeviceStatus } from "@/protoFleet/store";
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
