import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useMiner, useMinerDeviceStatus } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";

type MinerIssuesProps = {
  deviceIdentifier: string;
  onClick?: () => void;
};

/**
 * Builds a descriptive error summary based on component-specific error counts.
 * Examples: "1 hashboard error", "2 fan errors", "2 hashboard, 1 PSU errors"
 */
const buildErrorSummary = (miner: MinerStateSnapshot | undefined): string | null => {
  if (!miner) return null;

  const parts: string[] = [];

  if (miner.hashboardErrorCount > 0) {
    parts.push(`${miner.hashboardErrorCount} hashboard`);
  }
  if (miner.fanErrorCount > 0) {
    parts.push(`${miner.fanErrorCount} fan`);
  }
  if (miner.psuErrorCount > 0) {
    parts.push(`${miner.psuErrorCount} PSU`);
  }
  if (miner.controlBoardErrorCount > 0) {
    parts.push(`${miner.controlBoardErrorCount} control board`);
  }

  // Check for "other" errors (total minus known component types)
  const knownComponentErrors =
    miner.hashboardErrorCount + miner.fanErrorCount + miner.psuErrorCount + miner.controlBoardErrorCount;
  const otherErrors = miner.errorCount - knownComponentErrors;
  if (otherErrors > 0) {
    parts.push(`${otherErrors} other`);
  }

  if (parts.length === 0) return null;

  const totalErrors = miner.errorCount;
  const errorWord = totalErrors === 1 ? "error" : "errors";

  if (parts.length === 1) {
    return `${parts[0]} ${errorWord}`;
  }

  return `${parts.join(", ")} ${errorWord}`;
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
    summary = buildErrorSummary(miner);
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
