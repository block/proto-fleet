import { useCallback, useContext } from "react";
import { useNavigate } from "react-router-dom";

import { ApiContext } from "api";

import PoolStatus from "./PoolStatus";

import "./style.css";

const PoolStatusWrapper = () => {
  const navigate = useNavigate();
  const { poolsInfo, poolsInfoStatus } = useContext(ApiContext);

  const handleClickViewPools = useCallback(() => {
    navigate("/settings");
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
