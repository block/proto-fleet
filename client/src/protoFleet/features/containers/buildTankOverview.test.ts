import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { buildTankOverview, tankPath } from "./buildTankOverview";
import type { ContainerOverviewData } from "./hooks/useContainerOverview";
import { EquipmentCoverageSchema } from "@/protoFleet/api/generated/common/v1/equipment_coverage_pb";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  EntityKind,
  EntityRefSchema,
  InventoryItemSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

function fixture(): ContainerOverviewData {
  return {
    container: create(ContainerSchema, { id: 7n, name: "CT1", powerW: 999000 }),
    tanks: [
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.TANK, id: 81n },
        position: 2,
        assetCount: 2n,
        ancestors: [{ kind: EntityKind.CONTAINER, id: 7n }],
        hashrateReportingCount: 2n,
        powerReportingCount: 2n,
        hashrateThs: 200,
        powerKw: 6,
        efficiencyJth: 30,
        minTemperatureC: 60,
        maxTemperatureC: 70,
      }),
    ],
    modules: [1, 2].map((position) =>
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.MODULE, id: BigInt(100 + position) },
        position,
        name: `Module ${position}`,
        state: position === 1 ? "hashing" : "sleeping",
        ancestors: [
          { kind: EntityKind.CONTAINER, id: 7n },
          { kind: EntityKind.TANK, id: 81n },
        ],
      }),
    ),
  };
}

describe("native tank overview", () => {
  it("uses module equipment totals, preserves states and formats the temperature preference", () => {
    const view = buildTankOverview(fixture(), "81", "F")!;
    expect(view.title).toBe("CT1 Tank 2");
    expect(view.kpis.map((kpi) => kpi.value)).toEqual(["200.0 TH/s", "6.0 kW", "30.0 J/TH", "140.0 °F – 158.0 °F"]);
    expect(view.card.slots?.map((slot) => slot.state)).toEqual(["healthy", "sleeping"]);
  });

  it("uses one dedicated tank meter independently of module count", () => {
    const data = fixture();
    data.tanks[0].powerReportingCount = 1n;
    data.tanks[0].coverage = create(EquipmentCoverageSchema, {
      hashrate: { reporting: 6, expected: 6, topologyComplete: true },
      power: { reporting: 1, expected: 1, topologyComplete: true },
      temperature: { reporting: 2, expected: 2, topologyComplete: true },
    });
    const view = buildTankOverview(data, "81", "C")!;
    expect(view.kpis[1].label).toBe("Tank power");
    expect(view.kpis[1].value).toBe("6.0 kW");
    expect(view.incomplete).toBe(false);
  });

  it.each(["rack", "other container", "invalid ID"])("rejects a colliding %s", (reason) => {
    const data = fixture();
    if (reason === "rack") data.tanks[0].ref!.kind = EntityKind.RACK;
    if (reason === "other container") data.tanks[0].ancestors[0].id = 8n;
    expect(buildTankOverview(data, reason === "invalid ID" ? "81suffix" : "81", "C")).toBeUndefined();
  });

  it("excludes retired, foreign and wrong-kind modules from the grid and history membership", () => {
    const data = fixture();
    data.modules.push(
      create(InventoryItemSchema, {
        ...data.modules[0],
        ref: create(EntityRefSchema, { kind: EntityKind.MODULE, id: 103n }),
        state: "retired",
      }),
      create(InventoryItemSchema, {
        ...data.modules[0],
        ref: create(EntityRefSchema, { kind: EntityKind.MINER, id: 104n }),
      }),
      create(InventoryItemSchema, {
        ...data.modules[0],
        ref: create(EntityRefSchema, { kind: EntityKind.MODULE, id: 105n }),
        ancestors: [
          create(EntityRefSchema, { kind: EntityKind.CONTAINER, id: 8n }),
          create(EntityRefSchema, { kind: EntityKind.TANK, id: 81n }),
        ],
      }),
    );
    expect(buildTankOverview(data, "81", "C")!.modules.map((module) => module.ref?.id)).toEqual([101n, 102n]);
  });

  it.each(["stale", "failed"])("hides totals when %s", (reason) => {
    const data = fixture();
    if (reason === "stale") data.container.stale = true;
    const view = buildTankOverview(data, "81", "C", reason === "failed")!;
    expect(view.kpis.every((kpi) => kpi.value === "—")).toBe(true);
    expect(view.incomplete).toBe(true);
    expect(view.card.modules).toEqual(["unknown", "unknown"]);
  });

  it("shows partial server totals and uses efficiency from paired readings", () => {
    const data = fixture();
    data.tanks[0].powerReportingCount = 1n;
    data.tanks[0].powerKw = 3;
    data.tanks[0].efficiencyJth = 25;
    expect(
      buildTankOverview(data, "81", "C")!
        .kpis.map((kpi) => kpi.value)
        .slice(0, 3),
    ).toEqual(["200.0 TH/s", "3.0 kW", "25.0 J/TH"]);
    data.tanks[0].powerReportingCount = 2n;
    data.tanks[0].hashrateThs = 0;
    data.tanks[0].powerKw = 0;
    data.tanks[0].efficiencyJth = undefined;
    expect(
      buildTankOverview(data, "81", "C")!
        .kpis.map((kpi) => kpi.value)
        .slice(0, 3),
    ).toEqual(["0 TH/s", "0.0 kW", "—"]);
  });

  it("keeps available totals when a module is missing and marks coverage incomplete", () => {
    const data = fixture();
    data.modules.pop();
    const view = buildTankOverview(data, "81", "C")!;
    expect(view.kpis[0].value).toBe("200.0 TH/s");
    expect(view.incomplete).toBe(true);
  });

  it("encodes controller identifiers and retains the stable Fleet tank ID", () => {
    expect(tankPath("controller/one", "81")).toBe("/containers/controller%2Fone/tanks/81");
  });
});
