import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
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
  it("shows an independently supported main breaker when ordinary controls are unsupported", () => {
    const container = create(ContainerSchema, {
      controls: { supported: false, mainCircuitBreakerSupported: true, mainCircuitBreakerClosed: false },
    });
    const view = buildEquipmentOverview(container);
    expect(view.controls).toEqual([
      { id: "main_circuit_breaker", label: "Main circuit breaker", on: undefined, stateLabel: "Open" },
    ]);
    expect(view.controlsNotice).toBeUndefined();
  });
  it.each([false, undefined])("hides the breaker without advertised support (%s)", (supported) => {
    const container = create(ContainerSchema, {
      controls: { mainCircuitBreakerSupported: supported, mainCircuitBreakerClosed: true },
    });
    expect(buildEquipmentOverview(container).controls).toEqual([]);
  });
});

const coolingReadings = () => {
  const observedAt = timestampFromDate(new Date());
  return create(ContainerSchema, {
    plant: { observedAt, dryCoolerInletTemperatureC: 48 },
    fans: [
      { componentId: "fan-1", observedAt, speedPercent: 40 },
      { componentId: "fan-2", observedAt, speedPercent: 60 },
    ],
    controls: {
      observedAt,
      supported: true,
      controls: [
        { name: "dry_cooler", enabled: true },
        { name: "dry_cooler_auto", enabled: true },
      ],
      setpoints: [
        { name: "dry_cooler_auto_temp_c", value: 32, unit: "C" },
        { name: "dry_cooler_manual_speed_percent", value: 90, unit: "%" },
      ],
    },
  });
};

it("distinguishes measured inlet temperature and average fan speed from the automatic target", () => {
  const c = coolingReadings();
  const view = buildEquipmentOverview(c);
  expect(view.controls[0].metric).toBe("48°C inlet · 50% avg fan speed");
  expect(view.controls[1].metric).toBe("Target 32°C");
  const fahrenheit = buildEquipmentOverview(c, false, "F");
  expect(fahrenheit.controls[0].metric).toBe("118.4°F inlet · 50% avg fan speed");
  expect(fahrenheit.controls[1].metric).toBe("Target 89.6°F");
});

it("preserves measured zero and hides incomplete or stale dry cooler readings", () => {
  const c = coolingReadings();
  c.plant!.dryCoolerInletTemperatureC = 0;
  c.fans.forEach((f) => {
    f.speedPercent = 0;
  });
  expect(buildEquipmentOverview(c).controls[0].metric).toBe("0°C inlet · 0% avg fan speed");
  c.fans[0].speedPercent = undefined;
  expect(buildEquipmentOverview(c).controls[0].metric).toBe("0°C inlet");
  c.plant!.observedAt = timestampFromDate(new Date(Date.now() - 180_000));
  expect(buildEquipmentOverview(c).controls[0].metric).toBeUndefined();
  expect(buildEquipmentOverview(coolingReadings(), true).controls.every((c) => c.metric === undefined)).toBe(true);
  const staleFans = coolingReadings();
  staleFans.fans[0].stale = true;
  expect(buildEquipmentOverview(staleFans).controls[0].metric).toBe("48°C inlet");
});
