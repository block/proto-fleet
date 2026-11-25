import type { MinerError } from "@/protoOS/store/contract";

export const isPsuFaultCause = (cause: string | undefined) =>
  cause === "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY" ||
  cause === "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE";

const thermalCauses = new Set([
  "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE",
  "PSU_OVER_TEMPERATURE_FAULT",
  "PSU_OVER_TEMPERATURE_WARNING",
]);
const severityRank = { info: 0, warning: 1, critical: 2 };

/** One issue per module PSU/cause, with all reporting boards and strongest severity. */
export function groupPsuErrors(errors: MinerError[]): MinerError[] {
  const grouped = new Map<string, MinerError>();
  for (const error of errors) {
    if (error.component?.kind !== "psu") {
      grouped.set(error.id, error);
      continue;
    }
    const cause = thermalCauses.has(error.code) ? "PSU_OVER_TEMPERATURE" : error.code;
    const id = `${error.component.id}:${cause}`;
    const previous = grouped.get(id);
    const strongest = previous && severityRank[previous.severity] >= severityRank[error.severity] ? previous : error;
    const reporters = new Map((previous?.reportedBy ?? []).map((reporter) => [reporter.component.id, reporter]));
    for (const reporter of error.reportedBy ?? []) reporters.set(reporter.component.id, reporter);
    grouped.set(id, {
      ...strongest,
      id,
      reportedBy: reporters.size
        ? [...reporters.values()].sort((a, b) => a.label.localeCompare(b.label, undefined, { numeric: true }))
        : undefined,
    });
  }
  return [...grouped.values()];
}
