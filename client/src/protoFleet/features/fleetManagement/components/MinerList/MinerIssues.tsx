import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMiner, useMinerDeviceStatus } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";

type MinerIssuesProps = {
  deviceIdentifier: string;
  onClick?: () => void;
};

const MinerIssues = ({ deviceIdentifier, onClick }: MinerIssuesProps) => {
  const miner = useMiner(deviceIdentifier);
  const deviceStatusFromStore = useMinerDeviceStatus(deviceIdentifier || "");

  // Compute issue flags
  const needsAuthentication = miner?.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatusFromStore === DeviceStatus.NEEDS_MINING_POOL;
  const errorCount = miner?.errorCount ?? 0;
  const hasHardwareErrors = errorCount > 0;

  // Compute summary text
  let summary: string | null = null;
  if (needsAuthentication) {
    summary = "Needs authentication";
  } else if (needsMiningPool) {
    summary = "Pool required";
  } else if (hasHardwareErrors) {
    summary = `${errorCount} ${errorCount === 1 ? "error" : "errors"}`;
  }

  const hasIssues = needsAuthentication || needsMiningPool || hasHardwareErrors;

  // Show empty state if no issues
  if (!hasIssues || !summary) {
    return null;
  }

  // Issues should always be clickable (even for disabled rows)
  const isClickable = !!onClick;

  return (
    <div
      className={`flex items-center gap-2 ${isClickable ? "cursor-pointer hover:underline" : ""}`}
      onClick={isClickable ? onClick : undefined}
    >
      {hasHardwareErrors && !needsAuthentication && !needsMiningPool && <Alert width="w-4" />}
      {summary}
    </div>
  );
};

export default MinerIssues;
