import type { TankModuleState } from "./TankModuleGrid";
import type { ComponentStatusModalProps } from "@/shared/components/StatusModal/types";

/**
 * Minimal tank shape the status glance needs — a structural subset of
 * ContainerTank so this helper stays free of a component import (and testable
 * in isolation). Any object with these fields (e.g. a ContainerTank) is
 * accepted.
 */
export interface TankStatusInput {
  label: string;
  on: boolean;
  /** Grid dimensions; the card renders exactly cols * rows module slots. */
  cols: number;
  rows: number;
  /**
   * Module states in row-major order. Missing entries read healthy, matching
   * TankModuleGrid; entries beyond cols * rows are not rendered or counted.
   */
  modules: TankModuleState[];
  /** Temperature readout mirroring the card footer, e.g. "65.5°". */
  tempLabel?: string;
  /** Power readout mirroring the card footer, e.g. "12.3 kW". */
  powerLabel?: string;
}

const EMPTY = "—";

/**
 * Map a container tank to the shared ComponentStatusModalProps consumed by the
 * StatusModal framework's ComponentStatusModalContent — the same layer the
 * miner modal drills into for a per-component glance. "tank" is a first-class
 * component type in that framework (liquid-cooling icon), so the tank card's
 * (ⓘ) reuses it directly (icon, layout, metrics grid) rather than a bespoke
 * modal.
 *
 * Pure (no DOM): the summary copy and the metric mapping — the point of the
 * glance — are unit-tested. The module health breakdown is derived from the
 * `cols * rows` grid that the card's module bars render from; missing entries
 * use the card's healthy default and excess entries are ignored, so the card
 * and its glance cannot disagree on module totals or health. Temperature and
 * power mirror the card footer readouts. A powered-off tank reads every module
 * offline with a dashed temp and zero power.
 *
 * The full drill-down is the Subtank detail page (reached by clicking the tank
 * card body); this glance is the quick tank-level summary.
 */
export function toTankComponentStatus(tank: TankStatusInput): ComponentStatusModalProps {
  const modules = Array.from({ length: tank.cols * tank.rows }, (_, index) => tank.modules[index] ?? "healthy");
  const total = modules.length;
  const attention = modules.filter((module) => module === "attention").length;

  // When the tank's PDU is off, every module is offline regardless of its
  // stored health; a running tank splits into healthy vs needs-attention.
  const offline = tank.on ? 0 : total;
  const needsAttention = tank.on ? attention : 0;
  const online = tank.on ? total - attention : 0;

  const summary = !tank.on
    ? `${tank.label} is powered off`
    : needsAttention > 0
      ? `${tank.label} has ${needsAttention} ${needsAttention === 1 ? "module" : "modules"} needing attention`
      : `${tank.label} is operating normally`;

  return {
    summary,
    componentType: "tank",
    errors: [],
    metrics: [
      { label: "Modules online", value: `${online}/${total}` },
      { label: "Needs attention", value: `${needsAttention}` },
      { label: "Temperature", value: tank.on ? (tank.tempLabel ?? EMPTY) : EMPTY },
      { label: "Power", value: tank.on ? (tank.powerLabel ?? EMPTY) : `0.0 kW` },
    ],
    metadata: {
      status: {
        label: "Status",
        value: !tank.on ? "Off" : needsAttention > 0 ? "Needs attention" : "Running",
      },
      offline: { label: "Offline modules", value: `${offline}` },
    },
  };
}
