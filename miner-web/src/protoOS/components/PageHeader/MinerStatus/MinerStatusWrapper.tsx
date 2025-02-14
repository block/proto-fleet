import MinerStatus from "./MinerStatus";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext/useMinerStatus";

const MinerStatusWrapper = () => {
  const { errors } = useMinerStatus();

  return <MinerStatus errors={errors.errors} loading={errors.pending} />;
};

export default MinerStatusWrapper;
