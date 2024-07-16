import { useContext } from "react";

import { ApiContext } from "api";

const useApiContext = () => {
  const {
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
  } = useContext(ApiContext);

  return {
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
  };
};

export { useApiContext };
