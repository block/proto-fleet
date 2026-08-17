import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { parseCurtailmentTerminalScopes } from "@/protoFleet/api/curtailmentScopes";
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
