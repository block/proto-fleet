import { ReactNode, useMemo } from "react";

import Pools from "./Pools";
import ContentHeader from "@/shared/components/ContentHeader";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import { isValidPool } from "@/shared/components/MiningPools/utility";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface PoolChangeOptions {
  isDelete?: boolean;
}

interface MiningPoolsProps {
  title: string;
  children?: ReactNode;
  loading?: boolean;
  onChange: (pools: PoolInfo[], options?: PoolChangeOptions) => void;
  pools: PoolInfo[];
}

const MiningPools = ({ title, children, loading, onChange, pools }: MiningPoolsProps) => {
  const hasConfiguredPools = useMemo(() => pools.some((pool) => isValidPool(pool)), [pools]);

  return (
    <>
      {hasConfiguredPools && (
        <ContentHeader
          title={title}
          subtitle="Add up to 3 pools in order of priority. If a pool fails or is removed, your miner switches to the next available pool automatically."
          testId="mining-pool-title"
        />
      )}
      {children}
      {loading ? <ProgressCircular indeterminate /> : <Pools pools={pools} onChangePools={onChange} />}
    </>
  );
};

export default MiningPools;
