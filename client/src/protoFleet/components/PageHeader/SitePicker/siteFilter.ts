import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

// Wire shape carried by the three list-list requests (ListBuildings,
// ListDevices via MinerListFilter, ListDeviceSets). Site IDs ride as
// bigint to match the proto field type; ActiveSite stores the decimal
// string form because bigint isn't JSON-serializable.
export interface SiteFilterFields {
  siteIds: bigint[];
  includeUnassigned: boolean;
  matchNone?: boolean;
}

const EMPTY: SiteFilterFields = { siteIds: [], includeUnassigned: false };
const MATCH_NOTHING: SiteFilterFields = { siteIds: [], includeUnassigned: false, matchNone: true };

// Translates the topbar SitePicker selection into the additive
// site_ids / include_unassigned pair shared by the three list filters:
//   all         → both empty (server returns every row in the org)
//   site(id)    → siteIds=[id], includeUnassigned=false
//   unassigned  → siteIds=[],   includeUnassigned=true
export const siteFilterFromActive = (active: ActiveSite): SiteFilterFields => {
  switch (active.kind) {
    case "all":
      return EMPTY;
    case "site":
      return { siteIds: [BigInt(active.id)], includeUnassigned: false };
    case "unassigned":
      return { siteIds: [], includeUnassigned: true };
  }
};

export const intersectSiteFilters = (scope: SiteFilterFields, filter: SiteFilterFields): SiteFilterFields => {
  if (scope.matchNone || filter.matchNone) return MATCH_NOTHING;

  const scopeIsAll = scope.siteIds.length === 0 && !scope.includeUnassigned;
  if (scopeIsAll) return filter;

  const filterIsAll = filter.siteIds.length === 0 && !filter.includeUnassigned;
  if (filterIsAll) return scope;

  if (scope.includeUnassigned) {
    return filter.includeUnassigned ? scope : MATCH_NOTHING;
  }

  const filterIds = new Set(filter.siteIds.map(String));
  const siteIds = scope.siteIds.filter((id) => filterIds.has(id.toString()));
  return siteIds.length > 0 ? { siteIds, includeUnassigned: false } : MATCH_NOTHING;
};

export const isMatchNoneSiteFilter = (filter: SiteFilterFields): boolean => filter.matchNone === true;
