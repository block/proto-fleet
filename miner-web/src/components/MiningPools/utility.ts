import { PoolIndex, PoolInfo } from "./types";

// only url is required
export const isValidPool = (pool?: PoolInfo) => {
  return !!pool?.url;
};

export const getPoolType = (poolIndex: PoolIndex) => {
  return poolIndex === 0 ? "default" : "backup";
};
