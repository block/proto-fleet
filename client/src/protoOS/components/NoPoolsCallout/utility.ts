import type { Pool } from "@/protoOS/api/generatedApi";

type PoolCalloutInfo = Pick<Pool, "status" | "url">;

const livePoolStatusPattern = /alive|active/i;
const miningPoolsPathPattern = /^\/(?:miners\/[^/]+\/)?settings\/mining-pools$/i;

const normalizePathname = (pathname: string) => {
  const normalized = pathname.trim().replace(/\/+$/, "");
  return normalized || "/";
};

const isPoolLive = (pool?: PoolCalloutInfo) => livePoolStatusPattern.test(pool?.status ?? "");

const hasLivePool = (poolsInfo?: readonly PoolCalloutInfo[]) => poolsInfo?.some(isPoolLive) ?? false;

const hasNoLivePools = (poolsInfo?: readonly PoolCalloutInfo[]) => {
  return poolsInfo !== undefined && !hasLivePool(poolsInfo);
};

const hasConfiguredPools = (poolsInfo?: readonly PoolCalloutInfo[]) => {
  return poolsInfo?.some((pool) => !!pool?.url) ?? false;
};

const isMiningPoolsPath = (pathname: string) => miningPoolsPathPattern.test(normalizePathname(pathname));

export const getNoPoolsCalloutState = (poolsInfo: readonly PoolCalloutInfo[] | undefined, pathname: string) => {
  const arePoolsConfigured = hasConfiguredPools(poolsInfo);
  const noPoolsLive = hasNoLivePools(poolsInfo);

  return {
    arePoolsConfigured,
    noPoolsLive,
    shouldShowNoPoolsCallout: noPoolsLive && !(isMiningPoolsPath(pathname) && !arePoolsConfigured),
  };
};
