import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

// Wire shape carried by the three list-list requests (ListBuildings,
// ListDevices via MinerListFilter, ListDeviceSets). Site IDs ride as
// bigint to match the proto field type; ActiveSite stores the decimal
// string form because bigint isn't JSON-serializable.
export interface SiteFilterFields {
  siteIds: bigint[];
  includeUnassigned: boolean;
}

const EMPTY: SiteFilterFields = { siteIds: [], includeUnassigned: false };

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
