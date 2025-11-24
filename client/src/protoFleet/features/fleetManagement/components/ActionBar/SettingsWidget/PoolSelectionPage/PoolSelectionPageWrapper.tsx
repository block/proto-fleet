import PoolSelectionPage from "./PoolSelectionPage";
import type { MiningPool } from "./types";
import usePools from "@/protoFleet/api/usePools";

interface PoolSelectionPageWrapperProps {
  numberOfMiners: number;
  onDismiss: (poolsChanged: boolean) => void;
}

const PoolSelectionPageWrapper = ({
  numberOfMiners,
  onDismiss,
}: PoolSelectionPageWrapperProps) => {
  const { pools } = usePools();

  const availablePools: MiningPool[] = pools.map((pool) => ({
    poolId: pool.poolId.toString(),
    name: pool.poolName,
    poolUrl: pool.url,
    username: pool.username,
  }));

  return (
    <PoolSelectionPage
      numberOfMiners={numberOfMiners}
      availablePools={availablePools}
      onDismiss={onDismiss}
    />
  );
};

export default PoolSelectionPageWrapper;
