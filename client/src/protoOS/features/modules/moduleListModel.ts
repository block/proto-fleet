import { moduleStatus, type ModuleStatus } from "@/protoOS/features/tanks/tankView";
import {
  asEntityKey,
  type ContainerTopology,
  type MetricTelemetry,
  type MinerError,
  type ModuleNode,
  type TankNode,
  type TelemetryState,
} from "@/protoOS/store/contract";
import type { SortDirection } from "@/shared/components/List/types";

export type ModuleColumn =
  "name" | "position" | "status" | "issues" | "hashrate" | "temperature" | "power" | "efficiency";
export const moduleColumns: ModuleColumn[] = [
  "name",
  "position",
  "status",
  "issues",
  "hashrate",
  "temperature",
  "power",
  "efficiency",
];
export const sortableModuleColumns = new Set<ModuleColumn>(moduleColumns);
export interface ModuleListItem {
  id: string;
  module: ModuleNode;
  tank?: TankNode;
  name: string;
  position: string;
  status: ModuleStatus;
  hasErrors: boolean;
  issues: "present" | "none" | "unknown";
  issueCount: number | null;
  issuesComplete: boolean;
  hashrate: number | null;
  temperature: number | null;
  power: number | null;
  efficiency: number | null;
}

export const moduleName = (module: ModuleNode, tank?: TankNode) =>
  `T${tank?.slot ?? "?"}-P${module.location?.moduleSlot ?? "?"}`;

const latestValue = (metric?: MetricTelemetry): number | null => {
  const value = metric?.latest?.value;
  return value != null && Number.isFinite(value) ? value : null;
};

export const buildModuleRows = (
  topology: ContainerTopology,
  telemetry: TelemetryState,
  errors: MinerError[] = [],
): ModuleListItem[] =>
  [...topology.modules.values()].map((module) => {
    const tank = module.tankId ? topology.tanks.get(module.tankId) : undefined;
    const nodes = module.hashboardIds.flatMap((id) => {
      const node = topology.hashboards.get(id);
      return node ? [node] : [];
    });
    // Retain every board's evidence even when its physical position is unknown.
    // Missing CT1 positions still leave coverage incomplete.
    const occupiedSlots = new Set(nodes.map((node) => node.location?.slot));
    const readings = [
      ...module.hashboardIds.map((id) => telemetry.hashboards.get(id)),
      ...[1, 2, 3].filter((slot) => !occupiedSlots.has(slot)).map(() => undefined),
    ];
    const counts = readings.map((reading) => reading?.reportedIssueCount);
    const issuesComplete = counts.every((count) => count !== undefined && count !== null);
    const knownCount = counts.reduce<number>((sum, count) => sum + (count ?? 0), 0);
    const rollup = telemetry.rollupsByEntity.get(asEntityKey(module.id));
    const componentIds = new Set<string>([
      ...module.hashboardIds,
      ...module.psuIds,
      ...module.controlBoardIds,
      ...module.fanIds,
      ...nodes.flatMap((board) => board.asicIds),
    ]);
    const hasErrors =
      knownCount > 0 ||
      [
        ...readings,
        ...module.psuIds.map((id) => telemetry.psus.get(id)),
        ...module.fanIds.map((id) => telemetry.fans.get(id)),
      ].some((reading) => reading?.health === "fault" || reading?.health === "degraded") ||
      errors.some(
        (error) =>
          error.owner.id === module.id ||
          componentIds.has(error.owner.id) ||
          (error.component && (componentIds.has(error.component.id) || error.component.owner.id === module.id)),
      );

    return {
      id: module.id,
      module,
      tank,
      name: moduleName(module, tank),
      position: `${tank?.slot ?? "?"}-${module.location?.moduleSlot === undefined ? "?" : String(module.location.moduleSlot).padStart(2, "0")}`,
      status: moduleStatus(readings),
      hasErrors,
      issues: knownCount > 0 ? "present" : issuesComplete ? "none" : "unknown",
      issueCount: knownCount > 0 || issuesComplete ? knownCount : null,
      issuesComplete,
      hashrate: latestValue(rollup?.hashrate),
      temperature: latestValue(rollup?.temperature),
      power: latestValue(rollup?.power),
      efficiency: latestValue(rollup?.efficiency),
    };
  });

export const compareModuleRows = (
  a: ModuleListItem,
  b: ModuleListItem,
  field: ModuleColumn,
  direction: SortDirection,
): number => {
  const sign = direction === "asc" ? 1 : -1;
  const compareNumber = (left: number | null | undefined, right: number | null | undefined) =>
    left == null ? (right == null ? 0 : 1) : right == null ? -1 : (left - right) * sign;
  let comparison = 0;
  if (field === "name" || field === "position") {
    comparison =
      compareNumber(a.tank?.slot, b.tank?.slot) ||
      compareNumber(a.module.location?.moduleSlot, b.module.location?.moduleSlot);
  } else if (field === "hashrate" || field === "temperature" || field === "power" || field === "efficiency")
    comparison = compareNumber(a[field], b[field]);
  else if (field === "issues") comparison = compareNumber(a.issueCount, b.issueCount);
  else if (field === "status") comparison = a.status.localeCompare(b.status) * sign;
  return comparison || a.id.localeCompare(b.id);
};
