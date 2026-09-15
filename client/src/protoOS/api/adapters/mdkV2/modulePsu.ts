import type { V2Hashboard } from "./generated/generatedApi";
import {
  asComponentId,
  type MinerError,
  type ModuleId,
  type PsuHardware,
  type PsuTelemetry,
} from "@/protoOS/store/contract";

const freshest = <T>(values: T[], age: (value: T) => number | undefined): T | undefined =>
  values.reduce<T | undefined>((best, value) => {
    const sampleAge = age(value);
    const bestAge = best === undefined ? undefined : age(best);
    return best === undefined || (sampleAge ?? Infinity) < (bestAge ?? Infinity) ? value : best;
  }, undefined);

const metric = (value: number | undefined, units: "W" | "V" | "A" | "°C") => ({
  latest: { value: value !== undefined && Number.isFinite(value) ? value : null, units },
});

/** The three boards report snapshots of the same module-owned PSU. */
export const normalizeModulePsu = (moduleId: ModuleId, boards: V2Hashboard[]) => {
  const identity = boards.find((board) => board.psuIdentity)?.psuIdentity;
  const electrical = freshest(
    boards.flatMap((board) => board.health?.psuElectrical ?? []),
    (value) => value.sampleAgeMs,
  );
  const thermal = boards.flatMap((board) => board.health?.psuThermal ?? []);
  const temperatures = freshest(thermal, (value) => value.temperaturesAgeMs);
  const state = freshest(thermal, (value) => value.thermalStateAgeMs);
  if (!identity && !electrical && !thermal.length) return undefined;

  const id = asComponentId(`${moduleId}:psu:1`);
  const hardware: PsuHardware = {
    ref: { id, kind: "psu", owner: { kind: "module", id: moduleId } },
    slot: 1,
    serialNumber: identity?.serial,
    manufacturer: identity?.vendor,
    model: identity?.model,
    hardwareRevision: identity?.revision,
  };
  const readings = [
    temperatures?.inletTemperature,
    temperatures?.srHeatsinkTemperature,
    temperatures?.pfcHeatsinkTemperature,
  ].filter((value): value is number => value !== undefined && Number.isFinite(value));
  const telemetry: PsuTelemetry = {
    componentId: id,
    inputVoltage: metric(electrical?.inputVoltage, "V"),
    inputCurrent: metric(electrical?.inputCurrent, "A"),
    inputPower: metric(electrical?.inputPower, "W"),
    outputVoltage: metric(electrical?.outputVoltage, "V"),
    outputCurrent: metric(electrical?.outputCurrent, "A"),
    outputPower: metric(electrical?.outputPower, "W"),
    ambientTemperature: metric(temperatures?.inletTemperature, "°C"),
    averageTemperature: metric(
      readings.length ? readings.reduce((sum, value) => sum + value, 0) / readings.length : undefined,
      "°C",
    ),
    hotspotTemperature: metric(readings.length ? Math.max(...readings) : undefined, "°C"),
    health:
      state?.thermalStateValid !== true
        ? "unknown"
        : state.overTemperatureFault
          ? "fault"
          : state.overTemperatureWarning
            ? "degraded"
            : "ok",
  };
  const errors: MinerError[] = [];
  if (state?.thermalStateValid === true) {
    for (const [active, code, severity, message] of [
      [state.overTemperatureFault, "PSU_OVER_TEMPERATURE_FAULT", "critical", "PSU over temperature fault"],
      [state.overTemperatureWarning, "PSU_OVER_TEMPERATURE_WARNING", "warning", "PSU over temperature warning"],
    ] as const) {
      if (active)
        errors.push({
          id: `${id}:${code}`,
          code,
          severity,
          message,
          owner: hardware.ref.owner,
          component: hardware.ref,
          source: { kind: "psu", slot: 1 },
        });
    }
  }
  return { id, hardware, telemetry, errors };
};
