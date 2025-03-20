import { useCallback } from "react";

import PoolStatus from "./PoolStatus";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext/useMinerStatus";
import { PopoverProvider } from "@/shared/components/Popover";
import { useNavigate } from "@/shared/hooks/useNavigate";

const PoolStatusWrapper = () => {
  const navigate = useNavigate();
  const { poolsInfo, poolsInfoStatus } = useMinerStatus();

  const handleClickViewPools = useCallback(() => {
    navigate("/settings/mining-pools");
  }, [navigate]);

  return (
    <PopoverProvider>
      <PoolStatus
        poolsInfo={poolsInfo}
        loading={poolsInfoStatus.pending}
        onClickViewPools={handleClickViewPools}
      />
    </PopoverProvider>
  );
};

export default PoolStatusWrapper;
