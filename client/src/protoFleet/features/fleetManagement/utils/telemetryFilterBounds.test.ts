import { describe, expect, it } from "vitest";

import { TELEMETRY_FILTER_BOUNDS, telemetryFilterBoundsForProtoField } from "./telemetryFilterBounds";
import { NumericField } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

describe("TELEMETRY_FILTER_BOUNDS", () => {
  it("exposes bounds for the v1 UI categories", () => {
    expect(TELEMETRY_FILTER_BOUNDS.hashrate).toBeDefined();
    expect(TELEMETRY_FILTER_BOUNDS.efficiency).toBeDefined();
    expect(TELEMETRY_FILTER_BOUNDS.power).toBeDefined();
    expect(TELEMETRY_FILTER_BOUNDS.temperature).toBeDefined();
  });

  it.each(Object.entries(TELEMETRY_FILTER_BOUNDS))("%s bounds are finite and well-ordered", (_, b) => {
    expect(Number.isFinite(b.min)).toBe(true);
    expect(Number.isFinite(b.max)).toBe(true);
    expect(b.min).toBeLessThan(b.max);
    expect(b.unit).toBeTypeOf("string");
    expect(b.unit.length).toBeGreaterThan(0);
    expect(b.label).toBeTypeOf("string");
    expect(b.label.length).toBeGreaterThan(0);
  });

  it("emits display units that match what telemetry APIs return", () => {
    expect(TELEMETRY_FILTER_BOUNDS.hashrate.unit).toBe("TH/s");
    expect(TELEMETRY_FILTER_BOUNDS.efficiency.unit).toBe("J/TH");
    expect(TELEMETRY_FILTER_BOUNDS.power.unit).toBe("kW");
    expect(TELEMETRY_FILTER_BOUNDS.temperature.unit).toBe("°C");
  });
});

describe("telemetryFilterBoundsForProtoField", () => {
  it("maps each supported NumericField proto enum to a bounds entry", () => {
    expect(telemetryFilterBoundsForProtoField(NumericField.HASHRATE_THS)).toBe(TELEMETRY_FILTER_BOUNDS.hashrate);
    expect(telemetryFilterBoundsForProtoField(NumericField.EFFICIENCY_JTH)).toBe(TELEMETRY_FILTER_BOUNDS.efficiency);
    expect(telemetryFilterBoundsForProtoField(NumericField.POWER_KW)).toBe(TELEMETRY_FILTER_BOUNDS.power);
    expect(telemetryFilterBoundsForProtoField(NumericField.TEMPERATURE_C)).toBe(TELEMETRY_FILTER_BOUNDS.temperature);
  });

  it("returns undefined for unspecified or unmapped (voltage/current — backend supports, UI doesn't yet)", () => {
    expect(telemetryFilterBoundsForProtoField(NumericField.UNSPECIFIED)).toBeUndefined();
    expect(telemetryFilterBoundsForProtoField(NumericField.VOLTAGE_V)).toBeUndefined();
    expect(telemetryFilterBoundsForProtoField(NumericField.CURRENT_A)).toBeUndefined();
  });
});
