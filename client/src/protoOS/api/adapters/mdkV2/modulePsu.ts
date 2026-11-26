import type { V2Hashboard, V2PsuElectricalHealth, V2PsuThermalHealth } from "./generated/generatedApi";
import { isPsuFaultCause } from "./modulePsuErrors";
import {
  asComponentId,
  type ModuleId,
  type PsuHardware,
  type PsuObservationTime,
  type PsuTelemetry,
  type PsuThermalObservation,
} from "@/protoOS/store/contract";
import { projectPsuReadings } from "@/protoOS/store/utils/psuFreshness";
import { sourceObservationTime } from "@/protoOS/store/utils/readingFreshness";

const observedSamples = <T>(samples: { time: PsuObservationTime | undefined; values: T }[]) =>
  samples.flatMap(({ time, values }) => (time ? [{ ...time, values }] : []));

const metric = (value: number | undefined, units: "W" | "V" | "A" | "°C") => ({
  latest: { value: value !== undefined && Number.isFinite(value) ? value : null, units },
});

const electricalMetrics = (value?: V2PsuElectricalHealth) => ({
  inputVoltage: metric(value?.inputVoltage, "V"),
  inputCurrent: metric(value?.inputCurrent, "A"),
  inputPower: metric(value?.inputPower, "W"),
  outputVoltage: metric(value?.outputVoltage, "V"),
  outputCurrent: metric(value?.outputCurrent, "A"),
  outputPower: metric(value?.outputPower, "W"),
});

const temperatureMetrics = (value?: V2PsuThermalHealth) => {
  const readings = [value?.inletTemperature, value?.srHeatsinkTemperature, value?.pfcHeatsinkTemperature].filter(
    (reading): reading is number => reading !== undefined && Number.isFinite(reading),
  );
  return {
    ambientTemperature: metric(value?.inletTemperature, "°C"),
    inletTemperature: metric(value?.inletTemperature, "°C"),
    srHeatsinkTemperature: metric(value?.srHeatsinkTemperature, "°C"),
    pfcHeatsinkTemperature: metric(value?.pfcHeatsinkTemperature, "°C"),
    averageTemperature: metric(
      readings.length ? readings.reduce((sum, reading) => sum + reading, 0) / readings.length : undefined,
      "°C",
    ),
    hotspotTemperature: metric(readings.length ? Math.max(...readings) : undefined, "°C"),
  };
};

function thermalHealth(value: V2PsuThermalHealth): PsuThermalObservation["health"] {
  if (value.thermalStateValid !== true) return "unknown";
  if (value.overTemperatureFault) return "fault";
  return value.overTemperatureWarning ? "degraded" : "ok";
}

/** The three boards report snapshots of the same module-owned PSU. */
export const normalizeModulePsu = (moduleId: ModuleId, boards: V2Hashboard[], now = Date.now()) => {
  const identity = boards.find((board) => board.psuIdentity)?.psuIdentity;
  const electrical = boards.flatMap(({ health }) => {
    if (!health?.psuElectrical) return [];
    const value = health.psuElectrical;
    return [{ value, timestamp: health.timestamp }];
  });
  const thermal = boards.flatMap(({ health }) =>
    health?.psuThermal ? [{ value: health.psuThermal, timestamp: health.timestamp }] : [],
  );
  const hasReportedFault = boards.some(({ health }) =>
    [...(health?.errors ?? []), ...(health?.warnings ?? [])].some((issue) => isPsuFaultCause(issue.cause)),
  );
  if (!identity && !electrical.length && !thermal.length && !hasReportedFault) return undefined;

  const id = asComponentId(`${moduleId}:psu:1`);
  const hardware: PsuHardware = {
    ref: { id, kind: "psu", owner: { kind: "module", id: moduleId } },
    slot: 1,
    serialNumber: identity?.serial,
    manufacturer: identity?.vendor,
    model: identity?.model,
    hardwareRevision: identity?.revision,
    firmware: identity?.firmwareVersion ? { application: { version: identity.firmwareVersion } } : undefined,
  };
  const thermalStates = thermal.flatMap(({ value, timestamp }) => {
    const time = sourceObservationTime(timestamp, value.thermalStateAgeMs);
    return time ? [{ ...time, health: thermalHealth(value) }] : [];
  });
  const fallbackElectrical = electricalMetrics(electrical[0]?.value);
  const rawObservations: NonNullable<PsuTelemetry["rawObservations"]> = {
    electrical: observedSamples(
      electrical.map(({ value, timestamp }) => ({
        time: sourceObservationTime(timestamp, value.sampleAgeMs),
        values: electricalMetrics(value),
      })),
    ),
    fallbackElectrical,
    temperatures: observedSamples(
      thermal.map(({ value, timestamp }) => ({
        time: sourceObservationTime(timestamp, value.temperaturesAgeMs),
        values: temperatureMetrics(value),
      })),
    ),
    thermalState: thermalStates,
  };
  // Retain values for existing power totals even if none of the frames have a valid timestamp.
  const fallback: PsuTelemetry = {
    componentId: id,
    ...fallbackElectrical,
    ...temperatureMetrics(thermal[0]?.value),
    rawObservations,
  };
  const telemetry = { ...projectPsuReadings(fallback, now), rawObservations };
  return { id, hardware, telemetry, thermalStates };
};
