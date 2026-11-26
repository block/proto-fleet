import type { MinerError, MinerErrorObservations, PsuThermalObservation } from "../contract";
import { selectPsuObservation } from "./psuFreshness";
import { readingStatus } from "./readingFreshness";

export function thermalFaultSeverity(samples: PsuThermalObservation[], now: number) {
  const sample = selectPsuObservation(samples, now);
  if (!sample || readingStatus(sample.observedAt, now) !== "current") return undefined;
  if (sample.health === "fault") return "critical";
  if (sample.health === "degraded") return "warning";
  return undefined;
}

/** Board reports remain visible. Thermal evidence can add a fault or raise its severity. */
export function visibleMinerErrors({ reports, thermal }: MinerErrorObservations, now: number): MinerError[] {
  if (!thermal.length) return reports;
  const errors = new Map(reports.map((report) => [report.id, report]));
  for (const { id, component, samples } of thermal) {
    const severity = thermalFaultSeverity(samples, now);
    if (!severity) continue;
    const board = errors.get(id);
    if (board && (board.severity === "critical" || board.severity === severity)) continue;
    const critical = severity === "critical";
    errors.set(id, {
      ...board,
      id,
      component,
      owner: component.owner,
      source: { kind: "psu", slot: 1 },
      severity,
      code: critical ? "PSU_OVER_TEMPERATURE_FAULT" : "PSU_OVER_TEMPERATURE_WARNING",
      message: critical ? "PSU over temperature fault" : "PSU over temperature warning",
    });
  }
  return [...errors.values()];
}
