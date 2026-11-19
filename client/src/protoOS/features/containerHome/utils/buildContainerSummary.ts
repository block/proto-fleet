import type { IssueSummary, ModuleGrid } from "../types";
import type { buildContainerLive } from "./buildContainerHome";
import { formatReading } from "./buildContainerHome";
import { getScopeHealth } from "@/protoOS/components/StatusModal/scopeHealth";
import { boardStatus, moduleHashboards, moduleStatuses, tankModules } from "@/protoOS/features/tanks/tankView";
import type { HardwareState, MinerError, TelemetryState } from "@/protoOS/store/contract";
import { moduleRouteValue, tankRouteValue } from "@/protoOS/topologyRouteParams";
import type { StatProps } from "@/shared/components/Stat/types";
import { formatHashrateOrDash } from "@/shared/utils/telemetryFormat";

type Live = ReturnType<typeof buildContainerLive>;

export function buildSummaryKpis(live: Live): Omit<StatProps, "size">[] {
  const { hashrate, idealHashrate, rollup } = live;
  const ofIdeal =
    hashrate !== null && idealHashrate !== null && idealHashrate > 0 ? (hashrate / idealHashrate) * 100 : null;
  const total = rollup?.boardCount;
  const online = rollup?.onlineBoardCount;
  const boardsKnown = total !== undefined && online !== undefined;
  const inactive = boardsKnown ? total - online : 0;
  return [
    {
      label: "Hashrate",
      value: formatHashrateOrDash(hashrate),
      text: ofIdeal === null ? undefined : `${ofIdeal.toFixed(1)}% of ${formatHashrateOrDash(idealHashrate)} ideal`,
      chartPercentage: ofIdeal === null ? undefined : Math.min(100, ofIdeal),
    },
    { label: "Power", value: formatReading(live.power === null ? null : live.power / 1000, "kW") },
    {
      label: "Efficiency",
      value: formatReading(live.efficiency, "J/TH"),
      text: live.moduleEfficiency === null ? undefined : `Hashboards ${formatReading(live.moduleEfficiency, "J/TH")}`,
    },
    {
      label: "Hashboards active",
      value: boardsKnown ? `${online} / ${total}` : "—",
      text: inactive > 0 ? `${inactive} inactive` : undefined,
      chartPercentage: boardsKnown && total > 0 ? (online / total) * 100 : undefined,
    },
  ].map((stat) => ({ ...stat, preformatted: true }));
}

export function buildModuleGrid(
  hardware: HardwareState,
  telemetry: TelemetryState,
  errors: MinerError[],
  minerRoot: string,
  now = Date.now(),
): ModuleGrid | undefined {
  const topology = hardware.topology;
  if (topology?.kind !== "container") return undefined;
  const counts = Object.fromEntries(moduleStatuses.map((status) => [status, 0])) as ModuleGrid["counts"];
  let columns = 0;
  const tanks = [...topology.tanks.values()]
    .sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity))
    .map((tank, tankIndex) => {
      const label = tank.label?.trim() || `Tank ${tank.slot ?? tankIndex + 1}`;
      // Tank scope, as on the Tanks page, so a module never changes state between views.
      const health = getScopeHealth({ kind: "tank", id: tank.id }, hardware, telemetry, errors, now);
      let next = 0;
      const modules = tankModules(tank, topology).map((module) => {
        const slot = module.location?.moduleSlot;
        const column = slot !== undefined && slot > next ? slot : next + 1;
        next = column;
        columns = Math.max(columns, column);
        const state = health?.moduleStatuses.get(module.id) ?? "unknown";
        counts[state] += 1;
        return {
          id: String(module.id),
          column,
          name: `${label} module ${slot ?? column}`,
          to: `${minerRoot}/tanks/${tankRouteValue(tank)}/modules/${moduleRouteValue(module, tank.moduleIds.indexOf(module.id) + 1)}`,
          state,
          boards: moduleHashboards(module, topology)
            .sort((a, b) => (a.location?.slot ?? Infinity) - (b.location?.slot ?? Infinity))
            .map((board) => boardStatus(telemetry.hashboards.get(board.id))),
        };
      });
      return { id: String(tank.id), label, modules };
    });
  return { columns, tanks, counts };
}

const issueLabel = (error: MinerError) => {
  if (error.title) return error.title;
  const words = error.code
    .replace(/^[A-Z]+_ERROR_CAUSE_/, "")
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .replaceAll("_", " ")
    .toLowerCase();
  return (words.charAt(0).toUpperCase() + words.slice(1)).replace(/\bpsu\b/i, "PSU");
};

const componentNouns: Partial<Record<string, [string, string]>> = {
  hashboard: ["board", "boards"],
  psu: ["PSU", "PSUs"],
  fan: ["fan", "fans"],
  module: ["module", "modules"],
  container: ["container", "containers"],
};

/** Collapse repeated reports into one row per issue; informational entries are not issues. */
export function buildIssueSummary(errors: MinerError[]): IssueSummary {
  const groups = new Map<string, { label: string; severity: "critical" | "warning"; kind: string; ids: Set<string> }>();
  for (const error of errors) {
    if (error.severity === "info") continue;
    const label = issueLabel(error);
    const kind = error.component?.kind ?? error.source?.kind ?? error.owner.kind;
    const key = `${error.severity}:${kind}:${label}`;
    const group = groups.get(key) ?? { label, severity: error.severity, kind, ids: new Set<string>() };
    group.ids.add(String(error.component?.id ?? error.owner.id));
    groups.set(key, group);
  }
  const rows = [...groups.entries()].map(([id, group]) => {
    const [singular, plural] = componentNouns[group.kind] ?? ["location", "locations"];
    return {
      id,
      label: group.label,
      severity: group.severity,
      count: group.ids.size,
      affected: `${group.ids.size} ${group.ids.size === 1 ? singular : plural}`,
    };
  });
  rows.sort(
    (a, b) =>
      Number(b.severity === "critical") - Number(a.severity === "critical") ||
      b.count - a.count ||
      a.label.localeCompare(b.label),
  );
  return {
    critical: rows.filter((row) => row.severity === "critical").length,
    warning: rows.filter((row) => row.severity === "warning").length,
    groups: rows.map(({ id, label, severity, affected }) => ({ id, label, severity, affected })),
  };
}
