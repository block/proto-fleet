import type { ComponentStatusModalProps } from "@/shared/components/StatusModal/types";

/**
 * Minimal fan shape the status glance needs — a structural subset of
 * ContainerFan so this helper stays free of a component import (and testable in
 * isolation). Any object with these fields (e.g. a ContainerFan) is accepted.
 */
export interface FanStatusInput {
  label: string;
  on: boolean | undefined;
  /** Fan speed as a percentage of max, 0–100 (drives the speed readout). */
  speedPercent: number | undefined;
  /** Human-readable W readout, e.g. "3,200". */
  powerLabel: string;
}

/**
 * Map a container fan to the shared ComponentStatusModalProps consumed by the
 * StatusModal framework's ComponentStatusModalContent — the same layer the
 * miner modal drills into for a per-component glance. "fan" is a native
 * component type in that framework, so the fan card's (i) reuses it directly
 * (icon, layout, metrics grid) rather than a bespoke modal.
 *
 * Pure (no DOM): the summary copy and the W/speed metric mapping — the point of
 * the glance — are unit-tested. Speed and speed mirror the fan card footer so the
 * card and its glance always read the same numbers; a powered-off fan reads
 * zeroed metrics. No errors are surfaced (prototype fans are healthy); the
 * framework renders the info (not critical) icon when the error list is empty.
 */
export function toFanComponentStatus(fan: FanStatusInput): ComponentStatusModalProps {
  const speed = Math.round(Math.max(0, Math.min(100, fan.speedPercent ?? 0)));

  return {
    summary: fan.on ? `${fan.label} is operating normally` : `${fan.label} is powered off`,
    componentType: "fan",
    errors: [],
    metrics: [
      { label: "Power", value: `${fan.powerLabel} W` },
      { label: "Speed", value: fan.speedPercent === undefined ? "—" : `${speed}%` },
    ],
    metadata: {
      status: { label: "Status", value: fan.on ? "Running" : "Off" },
    },
  };
}
