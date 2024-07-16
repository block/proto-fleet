import { useCallback } from "react";
import { useNavigate } from "react-router-dom";

import { useApiContext } from "common/hooks/useApiContext";

import PoolStatus from "./PoolStatus";

const PoolStatusWrapper = () => {
  const navigate = useNavigate();
  const { poolsInfo, poolsInfoStatus } = useApiContext();

  const handleClickViewPools = useCallback(() => {
    navigate("/settings/mining-pools");
  }, [navigate]);

  return (
    <PoolStatus
      poolsInfo={poolsInfo}
      loading={poolsInfoStatus.pending}
      onClickViewPools={handleClickViewPools}
    />
  );
};

export default PoolStatusWrapper;
