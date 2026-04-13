import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import SingleMinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu";
import { Alert } from "@/shared/assets/icons";
import { useNeedsAttention } from "@/shared/hooks/useNeedsAttention";

type MinerNameProps = {
  miner: MinerStateSnapshot;
  errors: ErrorMessage[];
  onOpenStatusFlow: (deviceIdentifier: string) => void;
  miners?: Record<string, MinerStateSnapshot>;
  onRefetchMiners?: () => void;
};

const MinerName = ({ miner, errors, onOpenStatusFlow, miners, onRefetchMiners }: MinerNameProps) => {
  const deviceIdentifier = miner.deviceIdentifier;
  const name = miner.name || deviceIdentifier;
  const deviceStatus = miner.deviceStatus;

  const needsAuthentication = miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED;
  const needsMiningPool = deviceStatus === DeviceStatus.NEEDS_MINING_POOL;
  const hasFirmwareStatus = deviceStatus === DeviceStatus.UPDATING || deviceStatus === DeviceStatus.REBOOT_REQUIRED;
  const needsAttention = useNeedsAttention(needsAuthentication, needsMiningPool, errors, false, hasFirmwareStatus);

  return (
    <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3">
      <div className="min-w-0 truncate text-left" title={name}>
        {name}
      </div>
      <div className="flex items-center gap-2">
        {needsAttention && !needsAuthentication && (
          <button
            onClick={() => onOpenStatusFlow(deviceIdentifier)}
            className="cursor-pointer transition-opacity hover:opacity-80"
            aria-label="View issues"
          >
            <Alert width="w-4" className="text-red-500" />
          </button>
        )}
        <SingleMinerActionsMenu
          deviceIdentifier={deviceIdentifier}
          deviceStatus={deviceStatus}
          minerName={name}
          disabled={needsAuthentication}
          miners={miners}
          onRefetchMiners={onRefetchMiners}
        />
      </div>
    </div>
  );
};

export default MinerName;
