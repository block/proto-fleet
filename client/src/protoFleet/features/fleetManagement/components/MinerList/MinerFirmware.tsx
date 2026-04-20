import { INACTIVE_PLACEHOLDER } from "./constants";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type MinerFirmwareProps = {
  miner: MinerStateSnapshot;
};

const MinerFirmware = ({ miner }: MinerFirmwareProps) => {
  return <span>{miner.firmwareVersion ?? INACTIVE_PLACEHOLDER}</span>;
};

export default MinerFirmware;
