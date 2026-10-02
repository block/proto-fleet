import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { buildContainerOverview } from "./buildContainerOverview";
import type { ContainerOverviewData } from "./hooks/useContainerOverview";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  EntityKind,
  EntityRefSchema,
  InventoryItemSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

function fixture(): ContainerOverviewData {
  return {
    container: create(ContainerSchema, { id: 7n, hashrateHs: 2e15, powerW: 30000 }),
    tanks: [
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.TANK, id: 81n },
        position: 1,
        assetCount: 2n,
        powerKw: 24,
        minTemperatureC: 60,
        maxTemperatureC: 70,
      }),
    ],
    modules: [2, 1].map((position) =>
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.MODULE, id: BigInt(100 + position) },
        name: `Module ${position}`,
        position,
        state: position === 1 ? "hashing" : "attention",
        ancestors: [
          { kind: EntityKind.CONTAINER, id: 7n },
          { kind: EntityKind.TANK, id: 81n },
        ],
      }),
    ),
  };
}

describe("collected container presentation", () => {
  it("uses shared PH/s formatting and keeps container power separate from equipment power", () => {
    const view = buildContainerOverview(fixture(), "C");
    expect(view.kpis.map((kpi) => kpi.value)).toEqual(["2.00 PH/s", "30.0 kW", "15.0 J/TH", "1/2"]);
    expect(view.tanks[0].stats).toEqual(["1/2 modules hashing", "60.0 °C – 70.0 °C", "24.0 kW"]);
    expect(view.tanks[0].on).toBeUndefined();
    expect(view.tanks[0].slots?.map((slot) => slot.id)).toEqual(["101", "102"]);
  });

  it("distinguishes unavailable readings from measured zero", () => {
    const data = fixture();
    data.container.hashrateHs = undefined;
    data.container.powerW = undefined;
    expect(
      buildContainerOverview(data, "C")
        .kpis.slice(0, 3)
        .map((kpi) => kpi.value),
    ).toEqual(["—", "—", "—"]);
    data.container.hashrateHs = 0;
    data.container.powerW = 0;
    expect(
      buildContainerOverview(data, "C")
        .kpis.slice(0, 3)
        .map((kpi) => kpi.value),
    ).toEqual(["0 TH/s", "0.0 kW", "—"]);
  });

  it.each(["stale", "refresh error"])("preserves topology but hides current readings on %s", (reason) => {
    const data = fixture();
    data.container.stale = reason === "stale";
    const view = buildContainerOverview(data, "F", reason === "refresh error");
    expect(view.kpis.every((kpi) => kpi.value === "—")).toBe(true);
    expect(view.tanks[0].slots?.every((slot) => slot.state === "unknown")).toBe(true);
    expect(view.tanks[0].stats.slice(1)).toEqual(["—", "—"]);
  });

  it("does not claim complete health when an expected module is missing", () => {
    const data = fixture();
    data.modules.pop();
    const view = buildContainerOverview(data, "C");
    expect(view.kpis[3].value).toBe("—");
    expect(view.tanks[0].stats[0]).toContain("readings incomplete");
  });

  it("groups by typed tank identity, not a colliding controller ID or position", () => {
    const data = fixture();
    data.modules[0].ancestors = [
      create(EntityRefSchema, { kind: EntityKind.CONTAINER, id: 81n }),
      create(EntityRefSchema, { kind: EntityKind.TANK, id: 82n }),
    ];
    expect(buildContainerOverview(data, "C").tanks[0].slots?.map((slot) => slot.id)).toEqual(["101"]);
  });

  it("drops inventory items without a typed ref instead of throwing", () => {
    const data = fixture();
    data.tanks.push(create(InventoryItemSchema, { position: 2, assetCount: 1n }));
    data.modules.push(
      create(InventoryItemSchema, {
        name: "Module 3",
        position: 3,
        state: "hashing",
        ancestors: [{ kind: EntityKind.TANK, id: 81n }],
      }),
    );
    const view = buildContainerOverview(data, "C");
    expect(view.tanks.map((tank) => tank.id)).toEqual(["81"]);
    expect(view.tanks[0].slots?.map((slot) => slot.id)).toEqual(["101", "102"]);
  });

  it("keeps the row-major module list in step with the positioned slots", () => {
    const view = buildContainerOverview(fixture(), "C");
    expect(view.tanks[0].modules).toEqual(view.tanks[0].slots?.map((slot) => slot.state));
    expect(view.tanks[0].modules).toEqual(["healthy", "attention"]);
  });

  it("lays modules out eight per row in position order", () => {
    const data = fixture();
    data.tanks[0].assetCount = 16n;
    data.modules = Array.from({ length: 16 }, (_, index) => 16 - index).map((position) =>
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.MODULE, id: BigInt(100 + position) },
        name: `Module ${position}`,
        position,
        state: "hashing",
        ancestors: [{ kind: EntityKind.TANK, id: 81n }],
      }),
    );
    const tank = buildContainerOverview(data, "C").tanks[0];
    expect(tank.cols).toBe(8);
    expect(tank.rows).toBe(2);
    expect(tank.slots?.map((slot) => slot.id)).toEqual(Array.from({ length: 16 }, (_, index) => String(101 + index)));
    expect(tank.slots?.[7]).toMatchObject({ row: 1, column: 8 });
    expect(tank.slots?.[8]).toMatchObject({ row: 2, column: 1 });
    data.modules = data.modules.slice(0, 9);
    data.tanks[0].assetCount = 9n;
    expect(buildContainerOverview(data, "C").tanks[0].rows).toBe(2);
  });

  it("shows unknown health and converts temperature preferences", () => {
    const data = fixture();
    data.modules[0].state = "unknown";
    const view = buildContainerOverview(data, "F");
    expect(view.kpis[3].value).toBe("—");
    expect(view.tanks[0].slots?.[1].state).toBe("unknown");
    expect(view.tanks[0].stats[1]).toBe("140.0 °F – 158.0 °F");
  });
});
