import type { TankModuleState } from "@/protoFleet/features/containers/components/TankModuleGrid";
import type { ComponentStatusModalProps } from "@/shared/components/StatusModal/types";

/**
 * Minimal tank shape the status glance needs — a structural subset of
 * ContainerTank so this helper stays free of a component import (and testable
 * in isolation). Any object with these fields (e.g. a ContainerTank) is
 * accepted.
 */
export interface TankStatusInput {
  label: string;
  on: boolean | undefined;
  /** Grid dimensions; the card renders exactly cols * rows module slots. */
  cols: number;
  rows: number;
  /**
   * Module states in row-major order. Missing entries read unknown, matching
   * TankModuleGrid; entries beyond cols * rows are not rendered or counted.
   */
  modules: TankModuleState[];
  /** Temperature readout mirroring the card footer, e.g. "65.5°". */
  tempLabel?: string;
  /** Power readout mirroring the card footer, e.g. "12.3 kW". */
  powerLabel?: string;
}

const EMPTY = "—";

/** Summarize the rendered module slots; missing health and power state remain unknown. */
export function toTankComponentStatus(tank: TankStatusInput): ComponentStatusModalProps {
  const modules = Array.from({ length: tank.cols * tank.rows }, (_, index) => tank.modules[index] ?? "unknown");
  const total = modules.length;
  const off = tank.on === false;
  const needsAttention = off ? 0 : modules.filter((module) => module === "attention" || module === "isolated").length;
  const offline = off ? total : modules.filter((module) => module === "offline").length;
  const online = off ? 0 : modules.filter((module) => module === "healthy").length;
  const unknown = modules.filter((module) => module === "unknown").length;
  const sleeping = off ? 0 : modules.filter((module) => module === "sleeping").length;
  const status = off
    ? "Off"
    : tank.on === undefined
      ? "Unknown"
      : needsAttention || offline
        ? "Needs attention"
        : unknown || total === 0
          ? "Unknown"
          : sleeping
            ? "Sleeping modules"
            : "Running";
  const summary = off
    ? `${tank.label} is powered off`
    : tank.on === undefined
      ? `${tank.label} power state is unavailable`
      : needsAttention > 0
        ? `${tank.label} has ${needsAttention} ${needsAttention === 1 ? "module" : "modules"} needing attention`
        : offline > 0
          ? `${tank.label} has ${offline} offline ${offline === 1 ? "module" : "modules"}`
          : unknown || total === 0
            ? `${tank.label} has incomplete module readings`
            : sleeping > 0
              ? `${tank.label} has ${sleeping} sleeping ${sleeping === 1 ? "module" : "modules"}`
              : `${tank.label} is operating normally`;

  return {
    summary,
    componentType: "tank",
    errors: [],
    metrics: [
      { label: "Modules online", value: `${online}/${total}` },
      { label: "Needs attention", value: `${needsAttention}` },
      { label: "Temperature", value: off ? EMPTY : (tank.tempLabel ?? EMPTY) },
      { label: "Power", value: off ? `0.0 kW` : (tank.powerLabel ?? EMPTY) },
    ],
    metadata: {
      status: {
        label: "Status",
        value: status,
      },
      offline: { label: "Offline modules", value: `${offline}` },
      ...(!off && unknown ? { unknown: { label: "Unknown modules", value: `${unknown}` } } : {}),
    },
  };
}
