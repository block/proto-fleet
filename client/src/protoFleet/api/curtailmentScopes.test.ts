import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { parseCurtailmentTerminalScopes } from "@/protoFleet/api/curtailmentScopes";
import {
  CurtailmentScopeSchema,
  ScopeBuildingSchema,
  ScopeDeviceListSchema,
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

  it("rejects topology scopes until the canonical UI supports them", () => {
    const scopes = [
      create(CurtailmentScopeSchema, {
        scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId: 7n }) },
      }),
    ];

    expect(() => parseCurtailmentTerminalScopes(scopes)).toThrow("canonical scope UI");
  });
});
