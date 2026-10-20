import type { JsonObject } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { isCurrentObservation } from "./moduleDetail";
import type { HashboardSlot } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const object = (value: unknown): JsonObject | undefined =>
  value && typeof value === "object" && !Array.isArray(value) ? (value as JsonObject) : undefined;
const number = (value: unknown) => (typeof value === "number" && Number.isFinite(value) ? value : undefined);

/** The boards carry duplicate snapshots of one module-owned PSU, never additive power readings. */
export function modulePsu(slots: HashboardSlot[], moduleId: bigint, now: number) {
  const boards = slots.flatMap((slot) =>
    slot.occupancy === "occupied" && isCurrentObservation(slot.observedAt, now) && slot.board?.moduleId === moduleId
      ? [slot.board]
      : [],
  );
  const identity = boards.map((board) => object(board.metadata?.psuIdentity)).find((value) => value !== undefined);
  const current = boards.filter((board) => isCurrentObservation(board.observedAt, now));
  const freshest = (field: "psuElectrical" | "psuThermal", ageField: string) =>
    current
      .flatMap((board) => {
        const value = object(board.health?.[field]);
        const age = number(value?.[ageField] ?? 0);
        if (!value || age === undefined || age < 0) return [];
        const at = Number(board.observedAt!.seconds) * 1000 + board.observedAt!.nanos / 1e6 - age;
        return isCurrentObservation(timestampFromMs(at), now) ? [{ value, at }] : [];
      })
      .sort((a, b) => b.at - a.at)[0]?.value;
  const electrical = freshest("psuElectrical", "sampleAgeMs");
  const thermal = freshest("psuThermal", "temperaturesAgeMs");
  // Retain a known PSU card when its readings expire, with unavailable metrics.
  if (!identity && !boards.some((board) => object(board.health?.psuElectrical) || object(board.health?.psuThermal)))
    return undefined;
  const temperatures = [thermal?.inletTemperature, thermal?.srHeatsinkTemperature, thermal?.pfcHeatsinkTemperature]
    .map(number)
    .filter((value): value is number => value !== undefined);
  return {
    identity,
    inputVoltage: number(electrical?.inputVoltage),
    inputPower: number(electrical?.inputPower),
    outputVoltage: number(electrical?.outputVoltage),
    outputPower: number(electrical?.outputPower),
    averageTemperature: temperatures.length
      ? temperatures.reduce((sum, value) => sum + value, 0) / temperatures.length
      : undefined,
    highTemperature: temperatures.length ? Math.max(...temperatures) : undefined,
  };
}
