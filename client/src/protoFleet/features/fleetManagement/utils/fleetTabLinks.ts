type SiteChildTab = "buildings" | "racks" | "miners";
type BuildingChildTab = "racks" | "miners";

export const siteTabHref = (tab: SiteChildTab, siteId: bigint | string): string => `/fleet/${tab}?site=${siteId}`;

export const buildingTabHref = (tab: BuildingChildTab, buildingId: bigint | string): string =>
  `/fleet/${tab}?building=${buildingId}`;
