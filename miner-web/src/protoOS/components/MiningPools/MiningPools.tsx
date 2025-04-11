import { ReactNode } from "react";

import Pools from "./Pools";
import ContentHeader from "@/shared/components/ContentHeader";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import Spinner from "@/shared/components/Spinner";

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
    <div className="h-full max-w-[640px]">
      <ContentHeader
        title={title}
        subtitle="Enter your mining pool details below."
        testId="mining-pool-title"
      />
      {children}
      {loading ? <Spinner /> : <Pools pools={pools} onChangePools={onChange} />}
    </div>
  );
};

export default MiningPools;
