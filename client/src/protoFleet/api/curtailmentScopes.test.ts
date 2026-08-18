import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  type CurtailmentTerminalScope,
  getCurtailmentScopeSummary,
  parseCurtailmentTerminalScopes,
} from "@/protoFleet/api/curtailmentScopes";
import {
  CurtailmentScopeSchema,
  ScopeBuildingSchema,
  ScopeDeviceListSchema,
  ScopeGroupSchema,
  ScopeRackSchema,
  ScopeSiteSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

describe("parseCurtailmentTerminalScopes", () => {
  it("normalizes repeated selectors of one terminal type", () => {
    const scopes = [101n, 102n, 101n].map((siteId) =>
      create(CurtailmentScopeSchema, {
        scope: { case: "site", value: create(ScopeSiteSchema, { siteId }) },
      }),
    );

    expect(parseCurtailmentTerminalScopes(scopes)).toEqual({ type: "site", siteIds: ["101", "102"] });
  });

  it("normalizes explicit miner identifiers", () => {
    const scopes = [
      create(CurtailmentScopeSchema, {
        scope: {
          case: "deviceIdentifiers",
          value: create(ScopeDeviceListSchema, { deviceIdentifiers: ["miner-1", "miner-1", "miner-2"] }),
        },
      }),
    ];

    expect(parseCurtailmentTerminalScopes(scopes)).toEqual({
      type: "deviceIdentifiers",
      deviceIdentifiers: ["miner-1", "miner-2"],
    });
  });

  it("rejects mixed terminal selector types", () => {
    const scopes = [
      create(CurtailmentScopeSchema, {
        scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) },
      }),
      create(CurtailmentScopeSchema, {
        scope: { case: "site", value: create(ScopeSiteSchema, { siteId: 101n }) },
      }),
    ];

    expect(() => parseCurtailmentTerminalScopes(scopes)).toThrow("exactly one terminal selector type");
  });

  it("normalizes building selectors", () => {
    const scopes = [7n, 8n, 7n].map((buildingId) =>
      create(CurtailmentScopeSchema, {
        scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId }) },
      }),
    );

    expect(parseCurtailmentTerminalScopes(scopes)).toEqual({ type: "building", buildingIds: ["7", "8"] });
  });

  it("normalizes rack selectors", () => {
    const scopes = [7n, 8n, 7n].map((rackId) =>
      create(CurtailmentScopeSchema, {
        scope: { case: "rack", value: create(ScopeRackSchema, { rackId }) },
      }),
    );

    expect(parseCurtailmentTerminalScopes(scopes)).toEqual({ type: "rack", rackIds: ["7", "8"] });
  });

  it("normalizes group selectors", () => {
    const scopes = [7n, 8n, 7n].map((groupId) =>
      create(CurtailmentScopeSchema, {
        scope: { case: "group", value: create(ScopeGroupSchema, { groupId }) },
      }),
    );

    expect(parseCurtailmentTerminalScopes(scopes)).toEqual({ type: "group", groupIds: ["7", "8"] });
  });
});

describe("getCurtailmentScopeSummary", () => {
  it("uses the caller's fallback label for whole-org and empty selections", () => {
    const options = { fallbackLabel: "Whole fleet" };

    expect(getCurtailmentScopeSummary({ type: "wholeOrg" }, options)).toBe("Whole fleet");
    expect(getCurtailmentScopeSummary({ minerSelectionMode: "subset" }, options)).toBe("Whole fleet");
  });

  it("keeps explicit miners ahead of retained site selections", () => {
    expect(
      getCurtailmentScopeSummary(
        {
          minerSelectionMode: "subset",
          deviceIdentifiers: ["miner-1", "miner-2"],
          siteSelection: "site",
          siteIds: ["101"],
        },
        { fallbackLabel: "Whole fleet" },
      ),
    ).toBe("2 miners");
  });

  it("formats all-sites, named single-site, and multi-site selections", () => {
    const options = {
      fallbackLabel: "Whole fleet",
      getSiteLabel: (siteId: string) => (siteId === "101" ? "Austin, TX" : undefined),
    };

    expect(getCurtailmentScopeSummary({ siteSelection: "allSites", siteIds: ["101", "102"] }, options)).toBe(
      "All sites",
    );
    expect(getCurtailmentScopeSummary({ siteSelection: "allSites", siteIds: [] }, options)).toBe("All sites");
    expect(getCurtailmentScopeSummary({ siteSelection: "site", siteIds: ["101"] }, options)).toBe("Austin, TX");
    expect(getCurtailmentScopeSummary({ siteSelection: "site", siteIds: ["101", "102"] }, options)).toBe("2 sites");
  });

  const topologyCases: [CurtailmentTerminalScope, string][] = [
    [{ type: "building", buildingIds: ["7"] }, "1 building"],
    [{ type: "rack", rackIds: ["7", "8"] }, "2 racks"],
    [{ type: "group", groupIds: ["7", "8"] }, "2 groups"],
  ];

  it.each(topologyCases)("formats topology scope %j as %s", (scope, expected) => {
    expect(getCurtailmentScopeSummary(scope, { fallbackLabel: "Whole fleet" })).toBe(expected);
  });
});
