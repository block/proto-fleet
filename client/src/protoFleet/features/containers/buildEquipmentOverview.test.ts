import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { buildEquipmentOverview } from "./buildEquipmentOverview";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

describe("collected equipment presentation", () => {
  it("preserves zero, unknown, unsupported and independently stale observations", () => {
    const container = create(ContainerSchema, {
      stale: true,
      fans: [
        { componentId: "dry-fan-4", speedPercent: 0, powerW: 0, alarmActive: false },
        { componentId: "fan-2" },
        { componentId: "fan-3", speedPercent: 80, powerW: 500, stale: true },
      ],
      controls: {
        supported: true,
        controls: [{ name: "dry_cooler", enabled: false }, { name: "coolant_pump" }],
        setpoints: [{ name: "coolant_pump_speed_hz", value: 0, unit: "Hz" }],
        mainCircuitBreakerSupported: false,
      },
    });
    const view = buildEquipmentOverview(container);
    expect(view.fans[0]).toMatchObject({ id: "dry-fan-4", label: "Fan 4", speedPercent: 0, powerLabel: "0" });
    expect(view.fans[1]).toMatchObject({
      speedPercent: undefined,
      powerLabel: "—",
      statusLabel: "Readings unavailable",
    });
    expect(view.fans[2]).toMatchObject({ speedPercent: undefined, powerLabel: "—", statusLabel: "Stale" });
    expect(view.controls).toEqual([
      { id: "dry_cooler", label: "Dry cooler", on: false },
      { id: "coolant_pump", label: "Coolant pump", on: undefined, metric: "0 Hz setpoint" },
      { id: "main_circuit_breaker", label: "Main circuit breaker", on: undefined, stateLabel: "Unsupported" },
    ]);
  });
  it("hides readings on refresh failure without inventing AC temperature or commands", () => {
    const container = create(ContainerSchema, {
      fans: [{ componentId: "fan", powerW: 100, speedPercent: 50 }],
      controls: { controls: [{ name: "air_conditioner_1", enabled: true }], mainCircuitBreakerSupported: true },
    });
    expect(buildEquipmentOverview(container).controls[0].metric).toBeUndefined();
    const view = buildEquipmentOverview(container, true);
    expect(view.fans[0].speedPercent).toBeUndefined();
    expect(view.controls.every((c) => c.stateLabel === "Stale" && c.on === undefined)).toBe(true);
  });
  it("distinguishes an unavailable response from explicit lack of support", () => {
    const container = create(ContainerSchema);
    expect(buildEquipmentOverview(container).controlsNotice).toBe("Equipment states unavailable.");
    container.controls = create(ContainerSchema, { controls: { supported: false } }).controls;
    expect(buildEquipmentOverview(container).controlsNotice).toContain("unsupported");
  });
});
