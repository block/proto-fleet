import MinerMeasurement from "./MinerMeasurement";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getMinerMeasurement } from "@/protoFleet/features/fleetManagement/utils/getMinerMeasurement";

type MinerHashrateProps = {
  miner: MinerStateSnapshot;
};

const MinerHashrate = ({ miner }: MinerHashrateProps) => {
  const hashrate = getMinerMeasurement(miner, (m) => m.hashrate);

  return <MinerMeasurement measurement={hashrate} unit="TH/s" />;
};

export default MinerHashrate;
