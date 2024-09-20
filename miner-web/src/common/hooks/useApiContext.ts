import { useContext } from "react";

import { ApiContext } from "api";

const useApiContext = () => {
  const {
    errors,
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
  } = useContext(ApiContext);

  return {
    errors,
    fetchPoolsInfo,
    miningStatus,
    poolsInfo,
    poolsInfoStatus,
    setMiningStatus,
  };
};

export { useApiContext };
