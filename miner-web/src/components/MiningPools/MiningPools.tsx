import { ReactNode } from "react";

import ContentHeader from "components/ContentHeader";

import Pools from "./Pools";
import { PoolInfo } from "./types";

interface MiningPoolsProps {
  children?: ReactNode;
  onChange: (pools: PoolInfo[]) => void;
  pools: PoolInfo[];
}

const MiningPools = ({ children, onChange, pools }: MiningPoolsProps) => {
  return (
    <div className="max-w-[640px]">
      <ContentHeader
        title="Mining pool"
        subtitle="Enter your mining pool details below."
        testId="mining-pool-title"
      />
      {children}
      <Pools pools={pools} onChangePools={onChange} />
    </div>
  );
};

export default MiningPools;
