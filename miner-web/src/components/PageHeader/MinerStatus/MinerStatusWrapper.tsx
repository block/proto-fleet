import { useApiContext } from "common/hooks/useApiContext";

import MinerStatus from "./MinerStatus";

const MinerStatusWrapper = () => {
  const { errors } = useApiContext();

  return <MinerStatus errors={errors.errors} loading={errors.pending} />;
};

export default MinerStatusWrapper;
