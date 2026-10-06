import { INACTIVE_PLACEHOLDER } from "./constants";
import {
  type MinerStateSnapshot,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import Tooltip from "@/shared/components/Tooltip";
import { positions } from "@/shared/constants";

type MinerWorkerNameProps = {
  miner: MinerStateSnapshot;
};

const DEFAULT_WORKER_NAME_HELP_TEXT =
  "Fleet uses the miner MAC address when it cannot read a worker name from the miner. Use Update worker names to change it.";

const normalizeWorkerName = (value: string | undefined) => value?.trim().toLowerCase() ?? "";

const isDefaultWorkerName = (workerName: string, macAddress: string | undefined) => {
  const normalizedMacAddress = normalizeWorkerName(macAddress);

  return workerName !== "" && normalizedMacAddress !== "" && workerName === normalizedMacAddress;
};

const MinerWorkerName = ({ miner }: MinerWorkerNameProps) => {
  const trimmedWorkerName = miner.workerName?.trim() ?? "";
  const normalizedWorkerName = normalizeWorkerName(miner.workerName);
  const isDefault = isDefaultWorkerName(normalizedWorkerName, miner.macAddress);

  if (miner.pairingStatus === PairingStatus.AUTHENTICATION_NEEDED) {
    return null;
  }

  if (!trimmedWorkerName) {
    return <span>{INACTIVE_PLACEHOLDER}</span>;
  }

  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <span className="min-w-0 truncate">{trimmedWorkerName}</span>
      {isDefault ? (
        <span className="shrink-0 text-text-primary-50" aria-label="Default worker name" data-no-row-click>
          <Tooltip
            body={DEFAULT_WORKER_NAME_HELP_TEXT}
            position={positions["bottom left"]}
            icon="info"
            widthClassName="w-72"
          />
        </span>
      ) : null}
    </span>
  );
};

export default MinerWorkerName;
