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
  it("keeps topology-scoped events read-only until the modal can render them", () => {
    const scopes = [7n, 8n].map((buildingId) =>
      create(CurtailmentScopeSchema, {
        scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId }) },
      }),
    );

    expect(
      mapCurtailmentEventToFormValues(create(CurtailmentEventSchema, { scopes, scopeSchemaVersion: 1 })),
    ).toBeUndefined();
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

    expect(
      mapCurtailmentEventToFormValues(create(CurtailmentEventSchema, { scopes, scopeSchemaVersion: 1 })),
    ).toBeUndefined();
  });

  it("fails closed on unsupported scope schema versions", () => {
    const scopes = [
      create(CurtailmentScopeSchema, {
        scope: { case: "building", value: create(ScopeBuildingSchema, { buildingId: 7n }) },
      }),
    ];

    expect(
      mapCurtailmentEventToFormValues(create(CurtailmentEventSchema, { scopes, scopeSchemaVersion: 2 })),
    ).toBeUndefined();
  });
});
