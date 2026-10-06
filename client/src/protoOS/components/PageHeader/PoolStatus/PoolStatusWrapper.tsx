import { useCallback } from "react";

import { navigationItems } from "../../NavigationMenu/constants";
import PoolStatus from "./PoolStatus";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePoolsInfo } from "@/protoOS/store";
import { PopoverProvider } from "@/shared/components/Popover";
import { useNavigate } from "@/shared/hooks/useNavigate";

const PoolStatusWrapper = () => {
  const navigate = useNavigate();
  const poolsInfo = usePoolsInfo();
  const { minerRoot } = useMinerHosting();

  const handleClickViewPools = useCallback(() => {
    // Prefix minerRoot so the link stays inside the embedded miner view when
    // fleet-hosted (minerRoot is "" in standalone protoOS); an absolute path
    // would escape the embed and land on ProtoFleet's own pool settings.
    navigate(`${minerRoot}/${navigationItems.miningPools}`);
  }, [navigate, minerRoot]);

  return (
    <PopoverProvider>
      <PoolStatus poolsInfo={poolsInfo} loading={poolsInfo === undefined} onClickViewPools={handleClickViewPools} />
    </PopoverProvider>
  );
};

export default PoolStatusWrapper;
