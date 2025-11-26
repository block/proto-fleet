import { emptyPoolInfo, poolInfoAttributes } from "./constants";
import { PoolInfo } from "./types";
import { deepClone } from "@/shared/utils/utility";

// only url is required
export const isValidPool = (pool?: PoolInfo) => {
  return !!pool?.url;
};

// pools is an array of 3 PoolInfo objects
// for ProtoOS priority 0 is the default pool, then backups 1 and 2
// for ProtoFleet priority is any non-negative number (lower number = higher priority)
// [{url: "", username: "", password: "", priority: 0},
//  {url: "", username: "", password: "", priority: 1},
//  {url: "", username: "", password: "", priority: 2}]
export const getEmptyPoolsInfo = (startingPriority: number = 0) => {
  return [...Array(3)].map((_, index) => {
    const poolInfo = deepClone(emptyPoolInfo);
    poolInfo[poolInfoAttributes.priority] = startingPriority + index;
    return poolInfo;
  });
};
