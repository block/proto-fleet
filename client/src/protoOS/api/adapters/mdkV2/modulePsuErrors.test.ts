import { expect, test } from "vitest";
import {
  V2HashboardErrorCause as Cause,
  type V2GetContainerMapResponse,
  type V2Hashboard,
  V2MiningState,
} from "./generated/generatedApi";
import { toCanonicalContainer, toCanonicalErrors, toErrorObservations } from "./normalize";
import { getScopeHealth } from "@/protoOS/components/StatusModal/scopeHealth";
import useMinerStore from "@/protoOS/store/useMinerStore";

const supply = Cause.HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY;
const thermal = Cause.HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE;
const now = new Date().toISOString();
const wire = (modules: V2Hashboard[][]): V2GetContainerMapResponse => ({
  containerMap: {
    tanks: [
      {
        id: 1,
        modules: modules.map((boards, module) => ({
          id: module + 1,
          slots: boards.map((board, slot) => ({
            hashboard: {
              serial: `M${module + 1}-B${slot + 1}`,
              location: { hashboardPosition: slot + 1 },
              ...board,
            },
          })),
        })),
      },
    ],
  },
});

test("groups explicit PSU reports per module, retains reporters, and uses the highest severity", () => {
  const response = wire([
    [
      { health: { timestamp: now, errors: [{ cause: supply }, { cause: supply }] } },
      { health: { timestamp: now, warnings: [{ cause: supply }] } },
    ],
    [{ health: { timestamp: now, errors: [{ cause: supply }] } }],
  ]);
  const errors = toCanonicalErrors(response, "ct1");
  expect(errors).toHaveLength(2);
  expect(errors[0]).toMatchObject({
    component: { kind: "psu" },
    severity: "critical",
    message: "Power supply fault",
    reportedBy: [
      { label: "Board 1", component: { kind: "hashboard" }, observedAt: Date.parse(now) },
      { label: "Board 2", component: { kind: "hashboard" }, observedAt: Date.parse(now) },
    ],
  });
  expect(errors[0].component?.id).not.toBe(errors[1].component?.id);
  const fixture = toCanonicalContainer(response, {}, {}, "ct1");
  expect(fixture.hardware.psus.size).toBe(2);
  expect([...fixture.telemetry.psus.values()].every((psu) => psu.health === "unknown")).toBe(true);
});

test("does not attribute ordinary board current or voltage faults to the PSU", () => {
  const errors = toCanonicalErrors(
    wire([
      [
        {
          health: {
            errors: [
              { cause: Cause.HASHBOARD_ERROR_CAUSE_BOARD_OVER_CURRENT },
              { cause: Cause.HASHBOARD_ERROR_CAUSE_BOARD_OVER_VOLTAGE },
            ],
          },
        },
      ],
    ]),
    "ct1",
  );
  expect(errors).toHaveLength(2);
  expect(errors.every((error) => error.component?.kind === "hashboard")).toBe(true);
});

test("coalesces board overtemperature reports and PSU thermal flags into one issue", () => {
  const errors = toCanonicalErrors(
    wire([
      [
        {
          health: {
            timestamp: now,
            errors: [{ cause: thermal }],
            psuThermal: { thermalStateValid: true, overTemperatureFault: true, overTemperatureWarning: true },
          },
        },
      ],
    ]),
    "ct1",
  );
  expect(errors).toHaveLength(1);
  expect(errors[0]).toMatchObject({
    component: { kind: "psu" },
    severity: "critical",
    reportedBy: [{ label: "Board 1" }],
  });
});

test.each([false, true])("expires thermal evidence between polls (board warning present: %s)", (boardWarning) => {
  const at = Date.parse(now);
  const response = wire([
    [
      {
        miningState: V2MiningState.MINING_STATE_MINING,
        health: {
          timestamp: now,
          warnings: boardWarning ? [{ cause: thermal }] : [],
          psuThermal: { thermalStateValid: true, thermalStateAgeMs: 119_000, overTemperatureFault: true },
        },
      },
    ],
  ]);
  const { hardware, telemetry } = toCanonicalContainer(response, {}, {}, "ct1", at);
  const status = () => useMinerStore.getState().minerStatus;
  const errors = () => [...status().errorsByOwner.values()].flat();
  const scope = { kind: "container" } as const;
  status().setErrors(toErrorObservations(response, "ct1", {}, at), at);
  try {
    status().refreshThermalFaults(at + 999);
    expect(errors()).toMatchObject([{ severity: "critical" }]);
    expect(getScopeHealth(scope, hardware, telemetry, errors(), at + 999)?.needsAttention).toBe(true);
    status().refreshThermalFaults(at + 1_000);
    expect(getScopeHealth(scope, hardware, telemetry, errors(), at + 1_000)?.needsAttention).toBe(boardWarning);
    if (boardWarning) {
      expect(errors()).toMatchObject([{ code: thermal, severity: "warning", reportedBy: [{ label: "Board 1" }] }]);
    } else {
      expect(errors()).toEqual([]);
    }
  } finally {
    status().setErrors([]);
  }
});

test.each([false, true])(
  "recovers a thermal fault from a map received without a clock (board warning: %s)",
  (boardWarning) => {
    const at = Date.parse(now);
    const response = wire([
      [
        {
          health: {
            timestamp: now,
            warnings: boardWarning ? [{ cause: thermal }] : [],
            psuThermal: { thermalStateValid: true, overTemperatureFault: true },
          },
        },
      ],
    ]);
    const status = () => useMinerStore.getState().minerStatus;
    const errors = () => [...status().errorsByOwner.values()].flat();
    try {
      status().setErrors(toErrorObservations(response, "ct1", {}, NaN), NaN);
      expect(errors().map((error) => error.severity)).toEqual(boardWarning ? ["warning"] : []);
      status().refreshThermalFaults(at + 5_000);
      expect(errors()).toMatchObject([{ severity: "critical" }]);
      status().refreshThermalFaults(at + 120_000);
      expect(errors().map((error) => error.severity)).toEqual(boardWarning ? ["warning"] : []);
      status().setErrors([]);
      status().refreshThermalFaults(at);
      expect(errors()).toEqual([]);
    } finally {
      status().setErrors([]);
    }
  },
);

test("uses the latest eligible thermal state and rejects a future enclosing frame even when its adjusted age is current", () => {
  const at = Date.parse(now);
  const board = (offset: number, fault: boolean, age: number): V2Hashboard => ({
    health: {
      timestamp: new Date(at + offset).toISOString(),
      psuThermal: { thermalStateValid: true, overTemperatureFault: fault, thermalStateAgeMs: age },
    },
  });
  const response = wire([[board(-2_000, true, 0), board(-1_000, false, 0), board(10_000, true, 10_000)]]);
  expect(toCanonicalErrors(response, "ct1", {}, at)).toEqual([]);
  expect(toCanonicalErrors(response, "ct1", {}, at + 10_000)).toMatchObject([{ severity: "critical" }]);
});

test("keeps source observation time and stable identity across report reordering and recovery", () => {
  const response = wire([
    [
      { health: { timestamp: "2020-01-01T00:00:00Z", errors: [{ cause: supply }] } },
      { health: { errors: [{ cause: supply }] } },
    ],
  ]);
  const original = toCanonicalErrors(response, "ct1");
  expect(original[0]).toMatchObject({
    reportedBy: [{ observedAt: Date.parse("2020-01-01T00:00:00Z") }, { observedAt: undefined }],
  });
  response.containerMap!.tanks![0].modules![0].slots!.reverse();
  expect(toCanonicalErrors(response, "ct1")[0].id).toBe(original[0].id);
  for (const slot of response.containerMap!.tanks![0].modules![0].slots!) slot.hashboard!.health!.errors = [];
  expect(toCanonicalErrors(response, "ct1")).toEqual([]);
});
