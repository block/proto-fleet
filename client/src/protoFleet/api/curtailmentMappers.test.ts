import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";

import { mapCurtailmentEventToFormValues } from "@/protoFleet/api/curtailmentMappers";
import {
  CurtailmentEventSchema,
  CurtailmentScopeSchema,
  ScopeBuildingSchema,
  ScopeRackSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

describe("mapCurtailmentEventToFormValues", () => {
  it("hydrates topology scopes into the canonical form fields", () => {
    const scopes = [7n, 8n].map((buildingId) =>
      create(CurtailmentScopeSchema, {
        scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId }) },
      }),
    );

    const values = mapCurtailmentEventToFormValues(create(CurtailmentEventSchema, { scopes }));

    expect(values).toEqual(
      expect.objectContaining({
        scopeType: "building",
        scopeId: "2 buildings",
        siteIds: [],
        buildingTargetIds: ["7", "8"],
        rackTargetIds: [],
        groupTargetIds: [],
        deviceIdentifiers: [],
      }),
    );
  });

  it("fails closed when an event contains mixed scope types", () => {
    const scopes = [
      create(CurtailmentScopeSchema, {
        scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId: 7n }) },
      }),
      create(CurtailmentScopeSchema, {
        scope: { case: "rack", value: create(ScopeRackSchema, { rackId: 8n }) },
      }),
    ];

    expect(mapCurtailmentEventToFormValues(create(CurtailmentEventSchema, { scopes }))).toBeUndefined();
  });
});
