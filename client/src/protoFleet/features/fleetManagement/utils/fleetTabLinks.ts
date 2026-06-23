import { scopedPath } from "@/protoFleet/routing/siteScope";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

type SiteChildTab = "buildings" | "racks" | "miners";
type BuildingChildTab = "racks" | "miners";

export const siteTabHref = (tab: SiteChildTab, siteId: bigint | string): string => `/fleet/${tab}?site=${siteId}`;

export const buildingTabHref = (
  tab: BuildingChildTab,
  buildingId: bigint | string,
  activeSite?: ActiveSite,
): string => {
  const href = `/fleet/${tab}?building=${buildingId}`;
  return activeSite ? scopedPath(href, activeSite) : href;
};
