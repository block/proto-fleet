import MinerStatus from "./MinerStatus";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext/useMinerStatus";

const MinerStatusWrapper = () => {
  const { comprehensiveStatus } = useMinerStatus();

  return <MinerStatus status={comprehensiveStatus} />;
};

export default MinerStatusWrapper;
