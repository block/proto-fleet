import { useState } from "react";

import { deepClone } from "common/utils/utility";

import { emptyPoolInfo } from "./constants";
import MiningPoolsComponent from "./MiningPools";
import { PoolInfo } from "./types";

export const MiningPools = () => {
  // pools is an array of 3 PoolInfo objects
  // index 0 is the default pool, then backups 1 and 2
  // [{url: "", username: "", password: ""}, x3]
  const [pools, setPools] = useState<PoolInfo[]>(
    Array(3).fill(deepClone(emptyPoolInfo))
  );

  const onChangePools = (newPools: PoolInfo[]) => {
    setPools(newPools);
  };

  return <MiningPoolsComponent onChange={onChangePools} pools={pools} />;
};

export default {
  title: "Components/Mining Pools",
};
