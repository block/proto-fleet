import { ReactNode } from "react";

import Pools from "./Pools";
import { PoolInfo } from "./types";
import ContentHeader from "@/shared/components/ContentHeader";
import Spinner from "@/shared/components/Spinner";

interface MiningPoolsProps {
  children?: ReactNode;
  loading?: boolean;
  onChange: (pools: PoolInfo[]) => void;
  pools: PoolInfo[];
}

const MiningPools = ({
  children,
  loading,
  onChange,
  pools,
}: MiningPoolsProps) => {
  return (
    <div className="h-full max-w-[640px]">
      <ContentHeader
        title="Mining pool"
        subtitle="Enter your mining pool details below."
        testId="mining-pool-title"
      />
      {children}
      {loading ? <Spinner /> : <Pools pools={pools} onChangePools={onChange} />}
    </div>
  );
};

export default MiningPools;
