import { describe, expect, it } from "vitest";

import { buildingRackScope } from "./buildingRackScope";

describe("buildingRackScope", () => {
  it("scopes to the building's site and surfaces site-unassigned racks", () => {
    // A real site: fetch that site's racks plus site-unassigned racks (the
    // path a rack takes into a site). Racks in other sites are ineligible and
    // deliberately not fetched.
    expect(buildingRackScope(42n)).toEqual({ siteIds: [42n], includeUnassigned: true });
  });

  it("fetches only site-unassigned racks for a site-unassigned building", () => {
    // buildingSiteId 0 = the building has no site. Only site-unassigned racks
    // are eligible for it, which is exactly siteIds=[] + includeUnassigned.
    expect(buildingRackScope(0n)).toEqual({ siteIds: [], includeUnassigned: true });
  });

  it("derives from the building, independent of any header SitePicker state", () => {
    // The scope is a pure function of the building's site, so the same
    // building always yields the same scope regardless of the persisted
    // header selection — this is what keeps the unscoped /buildings/:id route
    // correct.
    expect(buildingRackScope(7n)).toEqual({ siteIds: [7n], includeUnassigned: true });
  });
});
