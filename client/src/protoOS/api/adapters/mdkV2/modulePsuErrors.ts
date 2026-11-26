import type { MinerError } from "@/protoOS/store/contract";

export const isPsuFaultCause = (cause: string | undefined) =>
  cause === "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY" ||
  cause === "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE";

export const thermalFaultId = (psuId: string) => `${psuId}:PSU_OVER_TEMPERATURE`;
const severityRank = { info: 0, warning: 1, critical: 2 };
const stronger = (left: MinerError, right: MinerError) =>
  severityRank[left.severity] >= severityRank[right.severity] ? left : right;

/** One issue per module PSU/cause, with all reporting boards and strongest severity. */
export function groupPsuErrors(errors: MinerError[]): MinerError[] {
  const grouped = new Map<string, MinerError[]>();
  for (const error of errors) {
    if (error.component?.kind !== "psu") {
      grouped.set(error.id, [error]);
      continue;
    }
    const id =
      error.code === "HASHBOARD_ERROR_CAUSE_BOARD_POWER_SUPPLY_OVER_TEMPERATURE"
        ? thermalFaultId(error.component.id)
        : `${error.component.id}:${error.code}`;
    const group = grouped.get(id);
    if (group) group.push(error);
    else grouped.set(id, [error]);
  }
  return Array.from(grouped, ([id, group]) => {
    const strongest = group.reduce(stronger);
    if (strongest.component?.kind !== "psu") return strongest;
    const reporters = new Map(group.flatMap((error) => error.reportedBy ?? []).map((r) => [r.component.id, r]));
    return {
      ...strongest,
      id,
      reportedBy: reporters.size
        ? [...reporters.values()].sort((a, b) => a.label.localeCompare(b.label, undefined, { numeric: true }))
        : undefined,
    };
  });
}
