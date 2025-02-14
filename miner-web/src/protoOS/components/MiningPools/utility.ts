import { emptyPoolInfo, info } from "./constants";
import { PoolIndex, PoolInfo } from "./types";
import { deepClone } from "@/shared/utils/utility";

// only url is required
export const isValidPool = (pool?: PoolInfo) => {
  return !!pool?.url;
};

export const getPoolType = (poolIndex: PoolIndex) => {
  return poolIndex === 0 ? "default" : "backup";
};

// pools is an array of 3 PoolInfo objects
// priority 0 is the default pool, then backups 1 and 2
// [{url: "", username: "", password: "", priority: 0},
//  {url: "", username: "", password: "", priority: 1},
//  {url: "", username: "", password: "", priority: 2}]
export const getEmptyPoolsInfo = () => {
  return [...Array(3)].map((_, index) => {
    const poolInfo = deepClone(emptyPoolInfo);
    poolInfo[info.priority] = index;
    return poolInfo;
  });
};
