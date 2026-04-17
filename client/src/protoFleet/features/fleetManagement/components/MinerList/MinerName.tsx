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
  onWorkerNameUpdated?: (deviceIdentifier: string, workerName: string) => void;
};

const MinerName = ({
  miner,
  errors,
  isActionLoading,
  onOpenStatusFlow,
  miners,
  onRefetchMiners,
  onWorkerNameUpdated,
}: MinerNameProps) => {
  const deviceIdentifier = miner.deviceIdentifier;
  const name = miner.name || deviceIdentifier;
  const deviceStatus = miner.deviceStatus;

  const needsAuthentication = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatus === DeviceStatus.NEEDS_MINING_POOL;
  const hasFirmwareStatus = deviceStatus === DeviceStatus.UPDATING || deviceStatus === DeviceStatus.REBOOT_REQUIRED;
  const needsAttention = useNeedsAttention(needsAuthentication, needsMiningPool, errors, false, hasFirmwareStatus);

  return (
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <div className={clsx("min-w-0 truncate text-left", { "opacity-50": needsAuthentication })} title={name}>
        {name}
      </div>
      <div className="flex items-center gap-2">
        {isActionLoading ? (
          <ProgressCircular size={14} indeterminate />
        ) : (
          needsAttention &&
          !needsAuthentication && (
            <button
              onClick={() => onOpenStatusFlow(deviceIdentifier)}
              className="cursor-pointer transition-opacity hover:opacity-80"
              aria-label="View issues"
            >
              <Alert width="w-4" className="text-red-500" />
            </button>
          )
        )}
        <SingleMinerActionsMenu
          deviceIdentifier={deviceIdentifier}
          minerUrl={miner.url || undefined}
          deviceStatus={deviceStatus}
          minerName={name}
          workerName={miner.workerName}
          needsAuthentication={needsAuthentication}
          miners={miners}
          onRefetchMiners={onRefetchMiners}
          onWorkerNameUpdated={onWorkerNameUpdated}
        />
      </div>
    </div>
  );
};

export default MinerName;
