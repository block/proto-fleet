import { boardStatus, moduleStatus, type ModuleStatus } from "@/protoOS/features/tanks/tankView";
import {
  asEntityKey,
  type ComponentRef,
  type HardwareState,
  type MinerError,
  type MiningStatus,
  type ModuleId,
  type TankId,
  type TelemetryState,
} from "@/protoOS/store/contract";
import { psuHealth } from "@/protoOS/store/utils/psuFreshness";

export type HealthScope = { kind: "container" } | { kind: "tank"; id: TankId } | { kind: "module"; id: ModuleId };

/** Topology determines membership; slots and display labels are never identities. */
export function getScopeHealth(
  scope: HealthScope,
  hardware: HardwareState,
  telemetry: TelemetryState,
  errors: MinerError[],
  now = Date.now(),
) {
  const topology = hardware.topology;
  if (topology?.kind !== "container") return undefined;
  const tank = scope.kind === "tank" ? topology.tanks.get(scope.id) : undefined;
  const selectedModule = scope.kind === "module" ? topology.modules.get(scope.id) : undefined;
  if ((scope.kind === "tank" && !tank) || (scope.kind === "module" && !selectedModule)) return undefined;
  const modules = [...topology.modules.values()].filter((module) =>
    scope.kind === "container" ? true : tank ? tank.moduleIds.includes(module.id) : module.id === selectedModule?.id,
  );
  const moduleIds = new Set(modules.map((module) => module.id));
  const scopedErrors = errors.filter(
    (error) => scope.kind === "container" || (error.owner.kind === "module" && moduleIds.has(error.owner.id)),
  );
  const errorComponents = new Set(scopedErrors.flatMap((error) => (error.component ? [error.component.id] : [])));
  const reportedComponents = new Set([
    ...errorComponents,
    ...scopedErrors.flatMap((error) => error.reportedBy?.map((reporter) => reporter.component.id) ?? []),
  ]);
  const affected = new Set(errorComponents);
  const observations: { component: ComponentRef; message: string }[] = [];
  const moduleStatuses = new Map<ModuleId, ModuleStatus>();
  // Mixed mining states can span whole modules, not just boards within one module.
  const scopePartiallyStopped =
    moduleStatus(modules.flatMap((module) => module.hashboardIds.map((id) => telemetry.hashboards.get(id)))) ===
    "attention";
  const observe = (component: ComponentRef, status: ModuleStatus, message: string) => {
    if (status === "attention" || status === "offline") affected.add(component.id);
    if (!reportedComponents.has(component.id) && ["attention", "offline", "unknown"].includes(status)) {
      observations.push({ component, message });
    }
  };
  for (const module of modules) {
    const boards = module.hashboardIds.map((id) => telemetry.hashboards.get(id));
    let status = moduleStatus(boards);
    const partiallyStopped = scopePartiallyStopped || status === "attention";
    module.hashboardIds.forEach((id) => {
      const reading = telemetry.hashboards.get(id);
      // Isolation is an operational choice; it must not hide reported faults.
      const state =
        reading?.health === "fault" || reading?.health === "degraded" || reading?.miningStatus === "error"
          ? "attention"
          : boardStatus(reading);
      const explainStopped = partiallyStopped && (state === "sleeping" || state === "isolated");
      if (state === "attention" || explainStopped) status = "attention";
      observe(
        { kind: "hashboard", id, owner: { kind: "module", id: module.id } },
        explainStopped ? "attention" : state,
        explainStopped
          ? state === "sleeping"
            ? "Sleeping"
            : "Isolated"
          : state === "attention"
            ? "Fault reported; details unavailable"
            : state === "offline"
              ? "Offline"
              : "Readings unavailable",
      );
    });
    if (
      (module.hashboardIds.length < 3 || module.psuIds.length === 0) &&
      status !== "attention" &&
      status !== "offline"
    )
      status = "unknown";
    for (const id of module.psuIds) {
      const health = psuHealth(telemetry.psus.get(id), now);
      const psuStatus =
        health === "fault" || health === "degraded"
          ? "attention"
          : health === "offline"
            ? "offline"
            : health === "ok"
              ? "healthy"
              : "unknown";
      observe(
        { kind: "psu", id, owner: { kind: "module", id: module.id } },
        psuStatus,
        psuStatus === "attention"
          ? "Fault reported; details unavailable"
          : psuStatus === "offline"
            ? "Offline"
            : "Readings unavailable",
      );
      if (status !== "attention" && psuStatus !== "healthy") {
        status = psuStatus === "attention" || status !== "offline" ? psuStatus : status;
      }
    }
    if (scopedErrors.some((error) => error.owner.kind === "module" && error.owner.id === module.id))
      status = "attention";
    moduleStatuses.set(module.id, status);
  }
  // Unlocated boards have container ownership; their faults must not disappear
  // when they are removed from physical module summaries.
  let unlocatedUnknown = false;
  if (scope.kind === "container") {
    for (const board of topology.hashboards.values()) {
      if (board.moduleId) continue;
      const reading = telemetry.hashboards.get(board.id);
      const status =
        reading?.health === "fault" || reading?.health === "degraded" || reading?.miningStatus === "error"
          ? "attention"
          : boardStatus(reading);
      unlocatedUnknown ||= status === "unknown";
      observe(
        { kind: "hashboard", id: board.id, owner: { kind: "container", id: topology.containerId } },
        status,
        reading ? "Location unknown" : "Readings unavailable",
      );
    }
  }
  const parentTank = selectedModule?.tankId ? topology.tanks.get(selectedModule.tankId) : tank;
  const label = selectedModule
    ? `${parentTank?.label ?? "Tank"} · Module ${selectedModule.location?.moduleSlot ?? selectedModule.id}`
    : (tank?.label ?? (tank ? `Tank ${tank.slot ?? tank.id}` : "Container"));
  const states = [...moduleStatuses.values()];
  const needsAttention = affected.size > 0 || scopedErrors.length > 0 || states.includes("attention");
  const unknown =
    unlocatedUnknown ||
    !states.length ||
    states.includes("unknown") ||
    (tank && modules.length !== tank.moduleIds.length);
  const title = needsAttention
    ? "Needs attention"
    : unknown
      ? "Status unknown"
      : states.every((state) => state === "sleeping")
        ? "Sleeping"
        : states.every((state) => state === "isolated")
          ? "Isolated"
          : "All components operational";
  const affectedModules = states.filter((state) => state === "attention" || state === "offline").length;
  const count = (value: number, word: string) => `${value} ${word}${value === 1 ? "" : "s"}`;
  const subtitle = needsAttention
    ? `${count(affected.size, "component")} affected${scope.kind === "module" || affectedModules === 0 ? "" : ` across ${count(affectedModules, "module")}`} · ${count(scopedErrors.length, "reported issue")}`
    : unknown
      ? "Some component readings are unavailable"
      : "No active component errors reported";
  const entityId = tank?.id ?? selectedModule?.id ?? topology.containerId;
  return {
    label,
    title,
    subtitle,
    errors: scopedErrors,
    observations,
    moduleStatuses,
    affectedComponents: affected.size,
    affectedModules,
    needsAttention,
    rollup: telemetry.rollupsByEntity.get(asEntityKey(entityId)),
  };
}

export const miningStatusLabels: Record<MiningStatus, string> = {
  uninitialized: "Not initialized",
  poweringOn: "Powering on",
  mining: "Mining",
  degradedMining: "Mining (needs attention)",
  poweringOff: "Powering off",
  stopped: "Stopped",
  curtailed: "Curtailed",
  noPools: "No pools",
  error: "Error",
  unknown: "Unknown",
};
