import type { V2Hashboard, V2PsuElectricalHealth, V2PsuThermalHealth } from "./generated/generatedApi";
import { isPsuFaultCause } from "./modulePsuErrors";
import {
  asComponentId,
  type ModuleId,
  type PsuHardware,
  type PsuObservationTime,
  type PsuThermalObservation,
} from "@/protoOS/store/contract";
import { projectPsuReadings, selectPsuObservation } from "@/protoOS/store/utils/psuFreshness";
import { sourceObservationTime } from "@/protoOS/store/utils/readingFreshness";
import { sensorReading } from "@/protoOS/store/utils/sensorReading";

const observedSamples = <T>(samples: { time: PsuObservationTime | undefined; values: T }[]) =>
  samples.flatMap(({ time, values }) => (time ? [{ ...time, values }] : []));

const electricalMetrics = (value?: V2PsuElectricalHealth) => ({
  inputVoltage: sensorReading(value?.inputVoltage, "V"),
  inputCurrent: sensorReading(value?.inputCurrent, "A"),
  inputPower: sensorReading(value?.inputPower, "W"),
  outputVoltage: sensorReading(value?.outputVoltage, "V"),
  outputCurrent: sensorReading(value?.outputCurrent, "A"),
  outputPower: sensorReading(value?.outputPower, "W"),
});

const temperatureMetrics = (value?: V2PsuThermalHealth) => {
  const readings = [value?.inletTemperature, value?.srHeatsinkTemperature, value?.pfcHeatsinkTemperature].filter(
    (reading): reading is number => reading !== undefined && Number.isFinite(reading),
  );
  return {
    ambientTemperature: sensorReading(value?.inletTemperature, "°C"),
    inletTemperature: sensorReading(value?.inletTemperature, "°C"),
    srHeatsinkTemperature: sensorReading(value?.srHeatsinkTemperature, "°C"),
    pfcHeatsinkTemperature: sensorReading(value?.pfcHeatsinkTemperature, "°C"),
    averageTemperature: sensorReading(
      readings.length ? readings.reduce((sum, reading) => sum + reading, 0) / readings.length : undefined,
      "°C",
    ),
    hotspotTemperature: sensorReading(readings.length ? Math.max(...readings) : undefined, "°C"),
  };
};

function thermalHealth(value: V2PsuThermalHealth): PsuThermalObservation["health"] {
  if (value.thermalStateValid !== true) return "unknown";
  if (value.overTemperatureFault) return "fault";
  return value.overTemperatureWarning ? "degraded" : "ok";
}

export const modulePsuRef = (moduleId: ModuleId) =>
  ({ id: asComponentId(`${moduleId}:psu:1`), kind: "psu", owner: { kind: "module", id: moduleId } }) as const;

export function psuThermalState({ health }: V2Hashboard): PsuThermalObservation[] {
  const value = health?.psuThermal;
  if (!value) return [];
  const time = sourceObservationTime(health.timestamp, value.thermalStateAgeMs);
  return time ? [{ ...time, health: thermalHealth(value) }] : [];
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

  const ref = modulePsuRef(moduleId);
  const hardware: PsuHardware = {
    ref,
    slot: 1,
    serialNumber: identity?.serial,
    manufacturer: identity?.vendor,
    model: identity?.model,
    hardwareRevision: identity?.revision,
    firmware: identity?.firmwareVersion ? { application: { version: identity.firmwareVersion } } : undefined,
  };
  const electricalSamples = observedSamples(
    electrical.map(({ value, timestamp }) => ({
      time: sourceObservationTime(timestamp, value.sampleAgeMs),
      values: electricalMetrics(value),
    })),
  );
  const rawObservations = {
    electrical: electricalSamples,
    // Keep the newest retained input for power totals when all frames are ineligible.
    fallbackElectrical: selectPsuObservation(electricalSamples, NaN)?.values ?? electricalMetrics(electrical[0]?.value),
    temperatures: observedSamples(
      thermal.map(({ value, timestamp }) => ({
        time: sourceObservationTime(timestamp, value.temperaturesAgeMs),
        values: temperatureMetrics(value),
      })),
    ),
    fallbackTemperatures: temperatureMetrics(thermal[0]?.value),
    thermalState: boards.flatMap(psuThermalState),
  };
  const telemetry = projectPsuReadings({ componentId: ref.id, rawObservations }, now);
  return { id: ref.id, hardware, telemetry: { ...telemetry, rawObservations } };
};
