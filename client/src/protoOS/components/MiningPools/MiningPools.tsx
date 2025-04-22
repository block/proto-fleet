import { ReactNode } from "react";

import Pools from "./Pools";
import ContentHeader from "@/shared/components/ContentHeader";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface MiningPoolsProps {
  title: string;
  children?: ReactNode;
  loading?: boolean;
  onChange: (pools: PoolInfo[]) => void;
  pools: PoolInfo[];
}

const MiningPools = ({
  title,
  children,
  loading,
  onChange,
  pools,
}: MiningPoolsProps) => {
  return (
    <div className="container mx-auto h-full max-w-[640px]">
      <ContentHeader
        title={title}
        subtitle="Enter your mining pool details below."
        testId="mining-pool-title"
      />
      {children}
      {loading ? (
        <ProgressCircular indeterminate />
      ) : (
        <Pools pools={pools} onChangePools={onChange} />
      )}
    </div>
  );
};

export default MiningPools;
