import { emptyPoolInfo, poolInfoAttributes } from "./constants";
import { PoolInfo } from "./types";
import { deepClone } from "@/shared/utils/utility";

export const isValidPool = (pool?: PoolInfo) => {
  return !!pool?.url;
};

export const getEmptyPoolsInfo = (startingPriority: number = 0) => {
  return [...Array(3)].map((_, index) => {
    const poolInfo = deepClone(emptyPoolInfo);
    poolInfo[poolInfoAttributes.priority] = startingPriority + index;
    return poolInfo;
  });
};
