import { expect, test } from "vitest";
import {
  V2HashboardErrorCause as Cause,
  type V2GetContainerMapResponse,
  type V2Hashboard,
} from "./generated/generatedApi";
import { toCanonicalContainer, toCanonicalErrors } from "./normalize";

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
