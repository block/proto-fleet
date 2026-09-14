import type { MiningScope } from "@/protoOS/api/adapters/types";
import type { ContainerTopology, HashboardTelemetry, ModuleNode, TankNode, Topology } from "@/protoOS/store/contract";

export const moduleStatuses = ["attention", "offline", "sleeping", "healthy", "unknown"] as const;
export type ModuleStatus = (typeof moduleStatuses)[number];
export const statusLabels: Record<ModuleStatus, string> = {
  attention: "Needs attention",
  offline: "Offline",
  sleeping: "Sleeping",
  healthy: "Healthy",
  unknown: "Unknown",
};

export const boardStatus = (board: HashboardTelemetry | undefined): ModuleStatus => {
  if (!board) return "unknown";
  if (board.health === "fault" || board.health === "degraded" || board.miningStatus === "error") return "attention";
  if (board.miningStatus === "stopped" || board.miningStatus === "curtailed") return "sleeping";
  if (board.health === "offline") return "offline";
  if (board.health === "ok" && board.miningStatus === "mining") return "healthy";
  return "unknown";
};

export const moduleStatus = (boards: (HashboardTelemetry | undefined)[]): ModuleStatus => {
  if (!boards.length) return "unknown";
  const statuses = boards.map(boardStatus);
  if (statuses.includes("attention")) return "attention";
  if (statuses.includes("offline")) return "offline";
  if (statuses.includes("unknown")) return "unknown";
  if (statuses.every((status) => status === "sleeping")) return "sleeping";
  return statuses.every((status) => status === "healthy") ? "healthy" : "attention";
};

export const tankModules = (tank: TankNode, topology: ContainerTopology) =>
  tank.moduleIds
    .flatMap((id) => {
      const module = topology.modules.get(id);
      return module ? [module] : [];
    })
    .sort((a, b) => (a.location?.moduleSlot ?? Infinity) - (b.location?.moduleSlot ?? Infinity));

export const moduleHashboards = (module: ModuleNode, topology: Topology) =>
  module.hashboardIds.map((id) => topology.hashboards.get(id)).filter((board) => board !== undefined);

export const tankMiningScope = (tank: TankNode, modules: ModuleNode[]): MiningScope | undefined => {
  if (!Number.isSafeInteger(tank.slot) || !modules.length || modules.length !== tank.moduleIds.length) return undefined;
  if (modules.some((module) => !Number.isSafeInteger(module.location?.moduleSlot))) return undefined;
  // An empty scope means the entire container to CT1. Never send it for a tank action.
  return { modules: modules.map((module) => ({ tank: tank.slot!, module: module.location!.moduleSlot! })) };
};

const DEFAULT_COLUMNS = 8;
const DEFAULT_ROWS = 2;

export const tankGrid = (tank: TankNode, modules: ModuleNode[]) => {
  const highestSlot = modules.reduce((max, module) => Math.max(max, module.location?.moduleSlot ?? 0), 0);
  const columns = tank.moduleLayout?.columns ?? DEFAULT_COLUMNS;
  const rows = tank.moduleLayout?.rows ?? Math.max(DEFAULT_ROWS, Math.ceil(highestSlot / columns));
  const numbering = tank.moduleLayout?.numbering ?? "rowMajor";
  return {
    columns,
    rows,
    positions: modules.map((module) => {
      const slot = module.location?.moduleSlot;
      if (!slot || slot < 1 || slot > rows * columns) return undefined;
      const index = slot - 1;
      return {
        column: numbering === "columnMajor" ? Math.floor(index / rows) + 1 : (index % columns) + 1,
        // The drawing is viewed from the service side: module 01 is at bottom left.
        row: rows - (numbering === "columnMajor" ? index % rows : Math.floor(index / columns)),
      };
    }),
  };
};

export const hashboardLabel = (slot: number | undefined) => {
  switch (slot) {
    case 1:
      return "left";
    case 2:
      return "mid";
    case 3:
      return "right";
    case undefined:
      return "unknown position";
    default:
      return String(slot);
  }
};
