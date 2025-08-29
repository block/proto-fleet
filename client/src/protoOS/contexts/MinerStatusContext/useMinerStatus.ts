import { useContext } from "react";

import MinerStatusContext from "./MinerStatusContext";
import useComprehensiveStatus from "./useComprehensiveStatus";

const useMinerStatus = () => {
  const {
    errors,
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
  } = useContext(MinerStatusContext);

  // boils down various status indicators into one comprehensive status
  const comprehensiveStatus = useComprehensiveStatus(
    errors.errors || [],
    miningStatus,
  );

  return {
    errors,
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
    comprehensiveStatus,
  };
};

export { useMinerStatus };
