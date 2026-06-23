import { describe, expect, it } from "vitest";

import { intersectSiteFilters, siteFilterFromActive } from "./siteFilter";

describe("siteFilter", () => {
  it("maps active site selections to request filter fields", () => {
    expect(siteFilterFromActive({ kind: "all" })).toEqual({ siteIds: [], includeUnassigned: false });
    expect(siteFilterFromActive({ kind: "site", id: "7" })).toEqual({ siteIds: [7n], includeUnassigned: false });
    expect(siteFilterFromActive({ kind: "unassigned" })).toEqual({ siteIds: [], includeUnassigned: true });
  });

  it("treats all-sites scope as identity", () => {
    expect(
      intersectSiteFilters({ siteIds: [], includeUnassigned: false }, { siteIds: [7n], includeUnassigned: false }),
    ).toEqual({ siteIds: [7n], includeUnassigned: false });
  });

  it("treats an empty URL site filter as identity", () => {
    expect(
      intersectSiteFilters({ siteIds: [7n], includeUnassigned: false }, { siteIds: [], includeUnassigned: false }),
    ).toEqual({ siteIds: [7n], includeUnassigned: false });
  });

  it("keeps overlapping site scope and URL site filter", () => {
    expect(
      intersectSiteFilters(
        { siteIds: [7n], includeUnassigned: false },
        { siteIds: [7n, 8n], includeUnassigned: false },
      ),
    ).toEqual({ siteIds: [7n], includeUnassigned: false });
  });

  it("returns an explicit match-nothing filter for disjoint site scope and URL site filter", () => {
    expect(
      intersectSiteFilters({ siteIds: [7n], includeUnassigned: false }, { siteIds: [8n], includeUnassigned: false }),
    ).toEqual({ siteIds: [], includeUnassigned: false, matchNone: true });
  });

  it("returns an explicit match-nothing filter for unassigned scope filtered to a site", () => {
    expect(
      intersectSiteFilters({ siteIds: [], includeUnassigned: true }, { siteIds: [8n], includeUnassigned: false }),
    ).toEqual({ siteIds: [], includeUnassigned: false, matchNone: true });
  });

  it("keeps unassigned scope when the URL filter includes unassigned and site IDs", () => {
    expect(
      intersectSiteFilters({ siteIds: [], includeUnassigned: true }, { siteIds: [8n], includeUnassigned: true }),
    ).toEqual({ siteIds: [], includeUnassigned: true });
  });
});
