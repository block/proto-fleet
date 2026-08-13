import { describe, expect, it } from "vitest";
import type { TankModuleState } from "./TankModuleGrid";
import { type TankStatusInput, toTankComponentStatus } from "./tankStatusModal";

/** 16 modules with `attention` needing-attention entries, rest healthy. */
function modules(total: number, attention: number): TankModuleState[] {
  return Array.from({ length: total }, (_, i) => (i < attention ? "attention" : "healthy"));
}

const healthyTank: TankStatusInput = {
  label: "Tank 1",
  on: true,
  cols: 8,
  rows: 2,
  modules: modules(16, 0),
  tempLabel: "65.5°",
  powerLabel: "12.3 kW",
};

describe("toTankComponentStatus", () => {
  it("maps a healthy tank to the shared tank component type with a normal summary", () => {
    const status = toTankComponentStatus(healthyTank);

    expect(status.componentType).toBe("tank");
    expect(status.summary).toBe("Tank 1 is operating normally");
    expect(status.errors).toEqual([]);
  });

  it("mirrors the module breakdown and footer readouts as metrics", () => {
    const status = toTankComponentStatus(healthyTank);

    expect(status.metrics).toEqual([
      { label: "Modules online", value: "16/16" },
      { label: "Needs attention", value: "0" },
      { label: "Temperature", value: "65.5°" },
      { label: "Power", value: "12.3 kW" },
    ]);
    expect(status.metadata?.status).toEqual({ label: "Status", value: "Running" });
    expect(status.metadata?.offline).toEqual({ label: "Offline modules", value: "0" });
  });

  it("counts needs-attention modules and pluralizes the summary", () => {
    const status = toTankComponentStatus({ ...healthyTank, label: "Tank 2", modules: modules(16, 2) });

    expect(status.summary).toBe("Tank 2 has 2 modules needing attention");
    expect(status.metrics?.[0]).toEqual({ label: "Modules online", value: "14/16" });
    expect(status.metrics?.[1]).toEqual({ label: "Needs attention", value: "2" });
    expect(status.metadata?.status).toEqual({ label: "Status", value: "Needs attention" });
  });

  it("uses the singular noun for a single needs-attention module", () => {
    const status = toTankComponentStatus({ ...healthyTank, label: "Tank 3", modules: modules(16, 1) });

    expect(status.summary).toBe("Tank 3 has 1 module needing attention");
  });

  it("reports every module offline with zeroed readouts for a powered-off tank", () => {
    const status = toTankComponentStatus({
      label: "Tank 6",
      on: false,
      cols: 8,
      rows: 2,
      modules: modules(16, 0),
      tempLabel: "65.0°",
      powerLabel: "12.4 kW",
    });

    expect(status.summary).toBe("Tank 6 is powered off");
    expect(status.metrics).toEqual([
      { label: "Modules online", value: "0/16" },
      { label: "Needs attention", value: "0" },
      { label: "Temperature", value: "—" },
      { label: "Power", value: "0.0 kW" },
    ]);
    expect(status.metadata?.status).toEqual({ label: "Status", value: "Off" });
    expect(status.metadata?.offline).toEqual({ label: "Offline modules", value: "16" });
  });

  it("uses the rendered grid size and the grid's healthy default for sparse module data", () => {
    const status = toTankComponentStatus({
      ...healthyTank,
      cols: 2,
      rows: 2,
      modules: ["attention", "healthy"],
    });

    expect(status.summary).toBe("Tank 1 has 1 module needing attention");
    expect(status.metrics?.[0]).toEqual({ label: "Modules online", value: "3/4" });
    expect(status.metrics?.[1]).toEqual({ label: "Needs attention", value: "1" });
    expect(status.metadata?.offline).toEqual({ label: "Offline modules", value: "0" });
  });

  it("ignores module entries beyond the rendered grid", () => {
    const status = toTankComponentStatus({
      ...healthyTank,
      cols: 1,
      rows: 1,
      modules: ["healthy", "attention"],
    });

    expect(status.summary).toBe("Tank 1 is operating normally");
    expect(status.metrics?.[0]).toEqual({ label: "Modules online", value: "1/1" });
    expect(status.metrics?.[1]).toEqual({ label: "Needs attention", value: "0" });
  });

  it("falls back to a dash when temp/power labels are missing", () => {
    const status = toTankComponentStatus({ ...healthyTank, tempLabel: undefined, powerLabel: undefined });

    expect(status.metrics?.[2]).toEqual({ label: "Temperature", value: "—" });
    expect(status.metrics?.[3]).toEqual({ label: "Power", value: "—" });
  });
});
