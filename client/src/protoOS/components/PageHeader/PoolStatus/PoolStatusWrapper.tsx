import { useCallback } from "react";

import PoolStatus from "./PoolStatus";
import { usePoolsInfo } from "@/protoOS/store";
import { PopoverProvider } from "@/shared/components/Popover";
import { useNavigate } from "@/shared/hooks/useNavigate";

const PoolStatusWrapper = () => {
  const navigate = useNavigate();
  const poolsInfo = usePoolsInfo();

  const handleClickViewPools = useCallback(() => {
    navigate("/settings/mining-pools");
  }, [navigate]);

  return (
    <PopoverProvider>
      <PoolStatus poolsInfo={poolsInfo} loading={poolsInfo === undefined} onClickViewPools={handleClickViewPools} />
    </PopoverProvider>
  );
};

export default PoolStatusWrapper;
