import { type IssueSummary, type ModuleGrid, type ModuleGridCell, overviewModuleStatuses } from "../types";
import { getScopeHealth } from "@/protoOS/components/StatusModal/scopeHealth";
import { moduleHashboards, tankGrid, tankModules } from "@/protoOS/features/tanks/tankView";
import type { HardwareState, HashboardTelemetry, MinerError, TelemetryState } from "@/protoOS/store/contract";
import { psuHealth } from "@/protoOS/store/utils/psuFreshness";
import { moduleRouteValue, tankRouteValue } from "@/protoOS/topologyRouteParams";

const faultSeverity = (board: HashboardTelemetry | undefined) =>
  board?.faultSeverity !== undefined
    ? board.faultSeverity
    : board?.health === "fault" || board?.miningStatus === "error"
      ? "critical"
      : board?.health === "degraded"
        ? "warning"
        : null;

const disconnected = (board: HashboardTelemetry | undefined) =>
  board?.disconnected ?? (board?.health === "offline" && !board.isolated);

const sleeping = (board: HashboardTelemetry | undefined) =>
  board?.miningStatus === "stopped" || board?.miningStatus === "poweringOff";

const boardDisplayState = (board: HashboardTelemetry | undefined): ModuleGridCell["boards"][number] => {
  if (disconnected(board)) return "offline";
  const severity = faultSeverity(board);
  if (severity) return severity === "critical" ? "attention" : "warning";
  if (sleeping(board)) return "sleeping";
  if (board?.miningStatus === "mining") return "healthy";
  return "unknown";
};

export function buildModuleGrid(
  hardware: HardwareState,
  telemetry: TelemetryState,
  errors: MinerError[],
  minerRoot: string,
  now = Date.now(),
): ModuleGrid | undefined {
  const topology = hardware.topology;
  if (topology?.kind !== "container") return undefined;
  const counts = Object.fromEntries(overviewModuleStatuses.map((status) => [status, 0])) as ModuleGrid["counts"];
  const tanks = [...topology.tanks.values()]
    .sort((a, b) => (a.slot ?? Infinity) - (b.slot ?? Infinity))
    .map((tank, tankIndex) => {
      const label = tank.label?.trim() || `Tank ${tank.slot ?? tankIndex + 1}`;
      // Retain scoped PSU/fault checks; the overview icon follows the miner status pill priority.
      const health = getScopeHealth({ kind: "tank", id: tank.id }, hardware, telemetry, errors, now);
      const members = tankModules(tank, topology);
      // Container overview follows the physical pairs: even slots above odd slots.
      const layout = tankGrid({ ...tank, moduleLayout: { columns: 8, rows: 2, numbering: "columnMajor" } }, members);
      const modules = members.map((module, index) => {
        const slot = module.location?.moduleSlot;
        const boardsBySlot = new Map(moduleHashboards(module, topology).map((board) => [board.location?.slot, board]));
        const readings = [1, 2, 3].map((slot) => {
          const board = boardsBySlot.get(slot);
          return board ? telemetry.hashboards.get(board.id) : undefined;
        });
        const boards = readings.map((reading, index) =>
          !boardsBySlot.has(index + 1) && module.unmappedHashboardSlots?.includes(index + 1)
            ? ("empty" as const)
            : boardDisplayState(reading),
        );
        const memberReadings = module.hashboardIds.map((id) => telemetry.hashboards.get(id));
        const complete = module.hashboardIds.length + boards.filter((board) => board === "empty").length === 3;
        const moduleErrors = errors.filter((error) => error.owner.kind === "module" && error.owner.id === module.id);
        const critical =
          memberReadings.some((board) => faultSeverity(board) === "critical") ||
          moduleErrors.some((error) => error.severity === "critical") ||
          module.psuIds.some((id) => psuHealth(telemetry.psus.get(id), now) === "fault");
        const warning =
          memberReadings.some((board) => faultSeverity(board) === "warning") ||
          moduleErrors.some((error) => error.severity === "warning") ||
          module.psuIds.some((id) => psuHealth(telemetry.psus.get(id), now) === "degraded");
        const scopedState = health?.moduleStatuses.get(module.id);
        const state = memberReadings.some(disconnected)
          ? "offline"
          : complete && memberReadings.length > 0 && memberReadings.every(sleeping)
            ? "sleeping"
            : critical || warning
              ? "attention"
              : complete &&
                  memberReadings.length > 0 &&
                  memberReadings.every((board) => board?.miningStatus === "mining") &&
                  module.psuIds.length > 0 &&
                  module.psuIds.every((id) => psuHealth(telemetry.psus.get(id), now) === "ok")
                ? "healthy"
                : scopedState === "isolated"
                  ? "unknown"
                  : (scopedState ?? "unknown");
        if (!boards.every((board) => board === "empty")) counts[state] += 1;
        return {
          id: String(module.id),
          label: slot === undefined ? String(module.id) : String(slot).padStart(2, "0"),
          position: layout.positions[index],
          name: `${label} module ${slot ?? module.id}`,
          to: `${minerRoot}/tanks/${tankRouteValue(tank)}/modules/${moduleRouteValue(module, tank.moduleIds.indexOf(module.id) + 1)}`,
          state,
          boards,
          attentionSeverity: warning && !critical ? ("warning" as const) : ("critical" as const),
        };
      });
      return { id: String(tank.id), label, slot: tank.slot, columns: layout.columns, rows: layout.rows, modules };
    });
  return { tanks, counts };
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
