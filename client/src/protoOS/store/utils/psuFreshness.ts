import type {
  ComponentId,
  LevelRollup,
  MinerError,
  PsuObservationTime,
  PsuTelemetry,
  PsuThermalObservation,
} from "../contract";

import { efficiencyValue } from "./efficiency";
import { readingStatus, type ReadingStatus } from "./readingFreshness";

export function selectPsuObservation<T extends PsuObservationTime>(
  observations: readonly T[],
  now: number,
): T | undefined {
  let newest: T | undefined;
  const clockKnown = Number.isFinite(now);
  for (const sample of observations) {
    // Check the enclosing frame so subtracting age cannot hide a future timestamp.
    if (clockKnown && readingStatus(sample.frameAt, now) === "unavailable") continue;
    if (!newest || sample.observedAt > newest.observedAt) newest = sample;
  }
  return newest;
}

const thermalHealth = (state: PsuThermalObservation | undefined, now: number) =>
  state && readingStatus(state.observedAt, now) === "current" ? state.health : "unknown";

export function retainedPsuInputPower(psu: PsuTelemetry, now: number): number | null {
  const raw = psu.rawObservations;
  const electrical = raw && selectPsuObservation(raw.electrical, now);
  return (electrical?.values ?? raw?.fallbackElectrical ?? psu).inputPower?.latest?.value ?? null;
}

export function psuRollupPower(
  ids: readonly ComponentId[] | null,
  readPower: (id: ComponentId) => number | null | undefined,
  hashrate: number | undefined,
): Required<Pick<LevelRollup, "power" | "efficiency" | "powerCoverage">> {
  let power: number | null = null;
  if (ids?.length) {
    let total = 0;
    for (const id of ids) {
      const value = readPower(id);
      if (value == null || !Number.isFinite(value)) {
        total = NaN;
        break;
      }
      total += value;
    }
    if (Number.isFinite(total)) power = total;
  }
  return {
    power: { latest: { value: power, units: "W" } },
    efficiency: { latest: { value: efficiencyValue(power ?? undefined, hashrate), units: "J/TH" } },
    powerCoverage: {
      kind: power === null ? "unknown" : "complete",
      includesNonHashboardLoads: true,
      methods: ["topologyMapping"],
    },
  };
}

export function projectPsuReadings(psu: PsuTelemetry, now: number): PsuTelemetry {
  if (!psu.rawObservations) return psu;
  const { rawObservations, ...base } = psu;
  const electrical = selectPsuObservation(rawObservations.electrical, now);
  const temperatures = selectPsuObservation(rawObservations.temperatures, now);
  const state = selectPsuObservation(rawObservations.thermalState, now);
  return {
    ...base,
    ...(electrical?.values ?? rawObservations.fallbackElectrical),
    ...temperatures?.values,
    observedAt: {
      electrical: electrical?.observedAt,
      temperatures: temperatures?.observedAt,
      thermalState: state?.health === "unknown" ? undefined : state?.observedAt,
    },
    health: thermalHealth(state, now),
  };
}

export function psuHealth(psu: PsuTelemetry | undefined, now = Date.now()) {
  if (psu?.rawObservations) return thermalHealth(selectPsuObservation(psu.rawObservations.thermalState, now), now);
  if (psu?.observedAt && readingStatus(psu.observedAt.thermalState, now) !== "current") return "unknown";
  return psu?.health ?? "unknown";
}

export function psuFreshness(psu: PsuTelemetry | undefined, now = Date.now()) {
  const observed = psu && projectPsuReadings(psu, now).observedAt;
  return {
    electrical: readingStatus(observed?.electrical, now),
    temperatures: readingStatus(observed?.temperatures, now),
    thermalState: readingStatus(observed?.thermalState, now),
  };
}

export function psuReadingNotice(label: string, status: ReadingStatus) {
  return status === "current" ? undefined : `${label} ${status}`;
}

export function psuFaultReportNotice(errors: MinerError[], now: number) {
  const statuses = errors
    .flatMap((error) => error.reportedBy ?? [])
    .map((reporter) => readingStatus(reporter.observedAt, now));
  return [
    statuses.includes("stale")
      ? statuses.every((status) => status === "stale")
        ? "Board fault reports are stale"
        : "Some board fault reports are stale"
      : undefined,
    statuses.includes("unavailable") ? "Board fault report age unavailable" : undefined,
  ]
    .filter(Boolean)
    .join(". ");
}

export function psuFaultMessage(error: MinerError, now: number) {
  if (!error.reportedBy?.length) return error.message;
  const reporters = error.reportedBy.map((reporter) => {
    const status = readingStatus(reporter.observedAt, now);
    return status === "current"
      ? reporter.label
      : `${reporter.label} (${status === "stale" ? "stale" : "age unavailable"})`;
  });
  return `${error.message} · Reported by ${reporters.join(", ")}`;
}
