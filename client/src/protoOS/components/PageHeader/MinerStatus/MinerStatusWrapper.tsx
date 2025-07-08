import MinerStatus from "./MinerStatus";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext/useMinerStatus";

const MinerStatusWrapper = () => {
  const { errors, miningStatus } = useMinerStatus();

  return (
    <MinerStatus
      errors={errors.errors}
      miningStatus={miningStatus}
      loading={errors.pending}
    />
  );
};

export default MinerStatusWrapper;
