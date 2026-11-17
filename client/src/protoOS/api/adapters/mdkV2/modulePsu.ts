import type { V2Hashboard } from "./generated/generatedApi";
import { isPsuFaultCause } from "./modulePsuErrors";
import {
  asComponentId,
  type MinerError,
  type ModuleId,
  type PsuHardware,
  type PsuTelemetry,
} from "@/protoOS/store/contract";
import { psuReadingStatus } from "@/protoOS/store/utils/psuFreshness";

const sampleTime = (timestamp: string | undefined, age = 0) => {
  const at = Date.parse(timestamp ?? "");
  return Number.isFinite(at) && at <= Date.now() + 5_000 && Number.isFinite(age) && age >= 0 ? at - age : undefined;
};

const freshest = <T>(values: { value: T; at: number | undefined }[]) =>
  values.reduce<(typeof values)[number] | undefined>(
    (best, sample) => (!best || (sample.at ?? -Infinity) > (best.at ?? -Infinity) ? sample : best),
    undefined,
  );

const metric = (value: number | undefined, units: "W" | "V" | "A" | "°C") => ({
  latest: { value: value !== undefined && Number.isFinite(value) ? value : null, units },
});

/** The three boards report snapshots of the same module-owned PSU. */
export const normalizeModulePsu = (moduleId: ModuleId, boards: V2Hashboard[]) => {
  const identity = boards.find((board) => board.psuIdentity)?.psuIdentity;
  const electrical = freshest(
    boards.flatMap(({ health }) =>
      health?.psuElectrical
        ? [{ value: health.psuElectrical, at: sampleTime(health.timestamp, health.psuElectrical.sampleAgeMs) }]
        : [],
    ),
  );
  const thermal = boards.flatMap(({ health }) =>
    health?.psuThermal ? [{ value: health.psuThermal, timestamp: health.timestamp }] : [],
  );
  const temperatures = freshest(
    thermal.map(({ value, timestamp }) => ({ value, at: sampleTime(timestamp, value.temperaturesAgeMs) })),
  );
  const state = freshest(
    thermal.map(({ value, timestamp }) => ({ value, at: sampleTime(timestamp, value.thermalStateAgeMs) })),
  );
  const hasReportedFault = boards.some(({ health }) =>
    [...(health?.errors ?? []), ...(health?.warnings ?? [])].some((issue) => isPsuFaultCause(issue.cause)),
  );
  if (!identity && !electrical && !thermal.length && !hasReportedFault) return undefined;

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
  const readings = [
    temperatures?.value.inletTemperature,
    temperatures?.value.srHeatsinkTemperature,
    temperatures?.value.pfcHeatsinkTemperature,
  ].filter((value): value is number => value !== undefined && Number.isFinite(value));
  const stateValid = state?.value.thermalStateValid === true && psuReadingStatus(state.at) === "current";
  const telemetry: PsuTelemetry = {
    componentId: id,
    observedAt: {
      electrical: electrical?.at,
      temperatures: temperatures?.at,
      thermalState: state?.value.thermalStateValid ? state.at : undefined,
    },
    inputVoltage: metric(electrical?.value.inputVoltage, "V"),
    inputCurrent: metric(electrical?.value.inputCurrent, "A"),
    inputPower: metric(electrical?.value.inputPower, "W"),
    outputVoltage: metric(electrical?.value.outputVoltage, "V"),
    outputCurrent: metric(electrical?.value.outputCurrent, "A"),
    outputPower: metric(electrical?.value.outputPower, "W"),
    ambientTemperature: metric(temperatures?.value.inletTemperature, "°C"),
    inletTemperature: metric(temperatures?.value.inletTemperature, "°C"),
    srHeatsinkTemperature: metric(temperatures?.value.srHeatsinkTemperature, "°C"),
    pfcHeatsinkTemperature: metric(temperatures?.value.pfcHeatsinkTemperature, "°C"),
    averageTemperature: metric(
      readings.length ? readings.reduce((sum, value) => sum + value, 0) / readings.length : undefined,
      "°C",
    ),
    hotspotTemperature: metric(readings.length ? Math.max(...readings) : undefined, "°C"),
    health: !stateValid
      ? "unknown"
      : state!.value.overTemperatureFault
        ? "fault"
        : state!.value.overTemperatureWarning
          ? "degraded"
          : "ok",
  };
  const errors: MinerError[] = [];
  if (stateValid) {
    for (const [active, code, severity, message] of [
      [state!.value.overTemperatureFault, "PSU_OVER_TEMPERATURE_FAULT", "critical", "PSU over temperature fault"],
      [state!.value.overTemperatureWarning, "PSU_OVER_TEMPERATURE_WARNING", "warning", "PSU over temperature warning"],
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
