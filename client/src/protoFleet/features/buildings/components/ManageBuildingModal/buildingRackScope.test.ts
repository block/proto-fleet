import { describe, expect, it } from "vitest";

import { assignedRackScope, buildingRackScope } from "./buildingRackScope";

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

describe("assignedRackScope", () => {
  it("scoped header → the building-site scope (same-site reparent only, no broaden)", () => {
    // A scoped header is guaranteed by scope-sync (#764) to equal the building's
    // own site, so the toggle-on fetch reuses the site scope. Other-building
    // same-site racks are already in that fetch — the toggle just stops hiding
    // them. No cross-site.
    expect(assignedRackScope(42n, false)).toEqual({ siteIds: [42n], includeUnassigned: true });
  });

  it("all-sites header → a global (unscoped) fetch so cross-site racks surface", () => {
    // The empty filter is the whole-org fetch; other-site racks appear and
    // become reparent candidates.
    expect(assignedRackScope(42n, true)).toEqual({ siteIds: [], includeUnassigned: false });
  });

  it("all-sites broadens regardless of the building's own site", () => {
    expect(assignedRackScope(0n, true)).toEqual({ siteIds: [], includeUnassigned: false });
  });
});
