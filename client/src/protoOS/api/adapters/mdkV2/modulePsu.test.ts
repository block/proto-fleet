import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { normalizeModulePsu } from "./modulePsu";
import { asModuleId } from "@/protoOS/store/contract";
import { projectPsuReadings, psuFreshness, psuHealth } from "@/protoOS/store/utils/psuFreshness";

const moduleId = asModuleId("container:ct1:module:1");
const now = Date.parse("2026-11-17T19:00:00Z");
const timestamp = (age = 0) => new Date(now - age).toISOString();

beforeEach(() => vi.useFakeTimers({ now }));
afterEach(() => vi.useRealTimers());

test("preserves PSU identity, individual temperatures and independent observation times", () => {
  const psu = normalizeModulePsu(moduleId, [
    {
      psuIdentity: { vendor: "Vendor", model: "Model", serial: "PSU-1", revision: "R2", firmwareVersion: "1.2.3" },
      health: {
        timestamp: timestamp(1_000),
        psuElectrical: { inputCurrent: 0, outputCurrent: 12.5, sampleAgeMs: 2_000 },
        psuThermal: {
          inletTemperature: 20,
          srHeatsinkTemperature: 50,
          pfcHeatsinkTemperature: 35,
          temperaturesAgeMs: 3_000,
          thermalStateValid: true,
          thermalStateAgeMs: 4_000,
        },
      },
    },
  ])!;
  expect(psu.hardware).toMatchObject({
    manufacturer: "Vendor",
    model: "Model",
    serialNumber: "PSU-1",
    hardwareRevision: "R2",
    firmware: { application: { version: "1.2.3" } },
  });
  expect(psu.telemetry).toMatchObject({
    inputCurrent: { latest: { value: 0 } },
    outputCurrent: { latest: { value: 12.5 } },
    inletTemperature: { latest: { value: 20 } },
    srHeatsinkTemperature: { latest: { value: 50 } },
    pfcHeatsinkTemperature: { latest: { value: 35 } },
    averageTemperature: { latest: { value: 35 } },
    hotspotTemperature: { latest: { value: 50 } },
    observedAt: { electrical: now - 3_000, temperatures: now - 4_000, thermalState: now - 5_000 },
    health: "ok",
  });
});

test("selects the newest actual PSU sample, not the smallest age on an older board report", () => {
  const psu = normalizeModulePsu(moduleId, [
    {
      health: {
        timestamp: timestamp(90_000),
        psuElectrical: { inputPower: 999, sampleAgeMs: 0 },
        psuThermal: { thermalStateValid: true, overTemperatureFault: true, thermalStateAgeMs: 0 },
      },
    },
    {
      health: {
        timestamp: timestamp(),
        psuElectrical: { inputPower: 0, sampleAgeMs: 1_000 },
        psuThermal: { thermalStateValid: true, thermalStateAgeMs: 1_000 },
      },
    },
  ])!;
  expect(psu.telemetry.inputPower?.latest?.value).toBe(0);
  expect(psu.telemetry.health).toBe("ok");
});

test.each([
  { timestamp: undefined, thermalStateAgeMs: 0 },
  { timestamp: "invalid", thermalStateAgeMs: 0 },
  { timestamp: timestamp(-6_000), thermalStateAgeMs: 0 },
  { timestamp: timestamp(110_000), thermalStateAgeMs: 10_000 },
  { timestamp: timestamp(), thermalStateAgeMs: -1 },
  { timestamp: timestamp(), thermalStateAgeMs: NaN },
])("does not claim thermal health from invalid or stale evidence: %j", (sample) => {
  const psu = normalizeModulePsu(moduleId, [
    {
      health: {
        timestamp: sample.timestamp,
        psuThermal: {
          thermalStateAgeMs: sample.thermalStateAgeMs,
          thermalStateValid: true,
          overTemperatureFault: true,
        },
      },
    },
  ])!;
  expect(psu.telemetry.health).toBe("unknown");
});

test("prefers the newest raw sample for retained power until the clock is known", () => {
  const { telemetry } = normalizeModulePsu(
    moduleId,
    [
      { health: { timestamp: "invalid", psuElectrical: { inputPower: 999 } } },
      { health: { timestamp: timestamp(90_000), psuElectrical: { inputPower: 123 } } },
      { health: { timestamp: timestamp(), psuElectrical: { inputPower: 0, sampleAgeMs: 1_000 } } },
    ],
    NaN,
  )!;
  expect(telemetry.inputPower?.latest?.value).toBe(0);
  expect(psuFreshness(telemetry, NaN).electrical).toBe("unavailable");
  expect(psuFreshness(telemetry, now).electrical).toBe("current");
  expect(projectPsuReadings(telemetry, now).inputPower?.latest?.value).toBe(0);
});

test("reselects electrical, temperature, and thermal state when a retained frame becomes valid", () => {
  const { telemetry } = normalizeModulePsu(
    moduleId,
    [
      {
        health: {
          timestamp: timestamp(9_000),
          psuElectrical: { inputPower: 100 },
          psuThermal: { inletTemperature: 30, thermalStateValid: true },
        },
      },
      {
        health: {
          timestamp: timestamp(-6_000),
          psuElectrical: { inputPower: 200, sampleAgeMs: 10_000 },
          psuThermal: {
            inletTemperature: 60,
            temperaturesAgeMs: 10_000,
            thermalStateAgeMs: 10_000,
            thermalStateValid: true,
            overTemperatureFault: true,
          },
        },
      },
    ],
    NaN,
  )!;
  expect(telemetry.rawObservations.thermalState).toEqual([
    { frameAt: now - 9_000, observedAt: now - 9_000, health: "ok" },
    { frameAt: now + 6_000, observedAt: now - 4_000, health: "fault" },
  ]);
  expect(psuHealth(telemetry, NaN)).toBe("unknown");
  const initial = projectPsuReadings(telemetry, now);
  expect(initial.inputPower?.latest?.value).toBe(100);
  expect(initial.inletTemperature?.latest?.value).toBe(30);
  expect(psuHealth(telemetry, now)).toBe("ok");

  const recovered = projectPsuReadings(telemetry, now + 6_000);
  expect(recovered.inputPower?.latest?.value).toBe(200);
  expect(recovered.inletTemperature?.latest?.value).toBe(60);
  expect(psuHealth(telemetry, now + 6_000)).toBe("fault");
  expect(projectPsuReadings(telemetry, now - 20_000).inputPower?.latest?.value).toBe(200);
});
