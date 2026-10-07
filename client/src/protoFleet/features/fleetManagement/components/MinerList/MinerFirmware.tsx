import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type MinerFirmwareProps = {
  miner: MinerStateSnapshot;
  onViewHistory?: () => void;
};

const MinerFirmware = ({ miner, onViewHistory }: MinerFirmwareProps) => {
  const version = miner.firmwareVersion?.trim() || "Unknown";

  if (!onViewHistory) return <span>{version}</span>;

  return (
    <button
      type="button"
      className="max-w-full cursor-pointer truncate rounded-sm text-left hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-core-primary-fill"
      aria-label={`View firmware history for ${miner.name || miner.deviceIdentifier}`}
      aria-haspopup="dialog"
      onClick={onViewHistory}
    >
      {version}
    </button>
  );
};

export default MinerFirmware;
