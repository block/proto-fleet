import { matchPath } from "react-router-dom";

import { settingsRouteMetadata } from "@/protoOS/routeAuth";

type PoolCalloutInfo = {
  status?: string;
  url?: string;
};

const livePoolStatusPattern = /alive|active/i;
const poolEmptyStateRoutePatterns = [
  "/pools",
  "/miners/:id/pools",
  `/settings/${settingsRouteMetadata.miningPools.path}`,
  `/miners/:id/settings/${settingsRouteMetadata.miningPools.path}`,
];

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

const hasPoolEmptyState = (pathname: string) => {
  const normalizedPathname = normalizePathname(pathname);

  return poolEmptyStateRoutePatterns.some((path) =>
    matchPath({ path, caseSensitive: false, end: true }, normalizedPathname),
  );
};

export const getNoPoolsCalloutState = (
  poolsInfo: readonly PoolCalloutInfo[] | undefined,
  pathname: string,
  mappingRunning = false,
) => {
  const arePoolsConfigured = hasConfiguredPools(poolsInfo);
  const noPoolsLive = hasNoLivePools(poolsInfo);

  return {
    arePoolsConfigured,
    noPoolsLive,
    shouldShowNoPoolsCallout:
      noPoolsLive && !(mappingRunning && arePoolsConfigured) && !(hasPoolEmptyState(pathname) && !arePoolsConfigured),
  };
};
