import clsx from "clsx";
import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import SingleMinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu";
import { Alert } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useNeedsAttention } from "@/shared/hooks/useNeedsAttention";

type MinerNameProps = {
  miner: MinerStateSnapshot;
  errors: ErrorMessage[];
  isActionLoading: boolean;
  onOpenStatusFlow: (deviceIdentifier: string) => void;
  miners?: Record<string, MinerStateSnapshot>;
  onRefetchMiners?: () => void;
  onRefreshMinersComplete?: () => void;
  onWorkerNameUpdated?: (deviceIdentifier: string, workerName: string) => void;
  onMergeMiners?: (snapshots: MinerStateSnapshot[]) => void;
  onMinerRefreshStateChange?: (deviceIdentifier: string, isRefreshing: boolean) => void;
};

const MinerName = ({
  miner,
  errors,
  isActionLoading,
  onOpenStatusFlow,
  miners,
  onRefetchMiners,
  onRefreshMinersComplete,
  onWorkerNameUpdated,
  onMergeMiners,
  onMinerRefreshStateChange,
}: MinerNameProps) => {
  const deviceIdentifier = miner.deviceIdentifier;
  const name = miner.name || deviceIdentifier;
  const deviceStatus = miner.deviceStatus;

  const needsAuthentication = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsPasswordChange = miner.pairingStatus === PairingStatus.DEFAULT_PASSWORD;
  const actionsRestricted = needsAuthentication || needsPasswordChange;
  const needsMiningPool = deviceStatus === DeviceStatus.NEEDS_MINING_POOL;
  const hasFirmwareStatus = deviceStatus === DeviceStatus.UPDATING || deviceStatus === DeviceStatus.REBOOT_REQUIRED;
  const needsAttention = useNeedsAttention(actionsRestricted, needsMiningPool, errors, false, hasFirmwareStatus);

  return (
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <div className={clsx("min-w-0 truncate text-left", { "opacity-50": needsAuthentication })} title={name}>
        {name}
      </div>
      <div className="flex items-center gap-2">
        {isActionLoading ? (
          <ProgressCircular size={14} indeterminate />
        ) : needsAttention && !needsAuthentication ? (
          <button
            onClick={() => onOpenStatusFlow(deviceIdentifier)}
            className="cursor-pointer transition-opacity hover:opacity-80"
            aria-label="View issues"
          >
            <Alert width="w-4" className="text-intent-critical-fill" />
          </button>
        ) : null}
        <SingleMinerActionsMenu
          deviceIdentifier={deviceIdentifier}
          minerUrl={miner.url || undefined}
          deviceStatus={deviceStatus}
          minerName={name}
          workerName={miner.workerName}
          needsAuthentication={actionsRestricted}
          allowSecurityAction={needsPasswordChange}
          miners={miners}
          onRefetchMiners={onRefetchMiners}
          onRefreshMinersComplete={onRefreshMinersComplete}
          onWorkerNameUpdated={onWorkerNameUpdated}
          onMergeMiners={onMergeMiners}
          onMinerRefreshStateChange={onMinerRefreshStateChange}
        />
      </div>
    </div>
  );
};

export default MinerName;
