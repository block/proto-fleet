import { useContext } from "react";

import { MinerStatusContext } from "./MinerStatusContext";

const useMinerStatus = () => {
  const {
    errors,
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
  } = useContext(MinerStatusContext);

  return {
    errors,
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
  };
};

export { useMinerStatus };
