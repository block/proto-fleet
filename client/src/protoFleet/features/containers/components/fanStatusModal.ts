import type { ComponentStatusModalProps } from "@/shared/components/StatusModal/types";

/**
 * Minimal fan shape the status glance needs — a structural subset of
 * ContainerFan so this helper stays free of a component import (and testable in
 * isolation). Any object with these fields (e.g. a ContainerFan) is accepted.
 */
export interface FanStatusInput {
  label: string;
  on: boolean;
  /** Fan speed as a percentage of max, 0–100 (drives the PWM readout). */
  speedPercent: number;
  /** Human-readable RPM readout, e.g. "3,200". */
  speedLabel: string;
}

/**
 * Map a container fan to the shared ComponentStatusModalProps consumed by the
 * StatusModal framework's ComponentStatusModalContent — the same layer the
 * miner modal drills into for a per-component glance. "fan" is a native
 * component type in that framework, so the fan card's (i) reuses it directly
 * (icon, layout, metrics grid) rather than a bespoke modal.
 *
 * Pure (no DOM): the summary copy and the RPM/PWM metric mapping — the point of
 * the glance — are unit-tested. Speed and PWM mirror the fan card footer so the
 * card and its glance always read the same numbers; a powered-off fan reads
 * zeroed metrics. No errors are surfaced (prototype fans are healthy); the
 * framework renders the info (not critical) icon when the error list is empty.
 */
export function toFanComponentStatus(fan: FanStatusInput): ComponentStatusModalProps {
  const pwm = Math.round(Math.max(0, Math.min(100, fan.speedPercent)));

  return {
    summary: fan.on ? `${fan.label} is operating normally` : `${fan.label} is powered off`,
    componentType: "fan",
    errors: [],
    metrics: [
      { label: "Speed", value: fan.on ? `${fan.speedLabel} RPM` : "0 RPM" },
      { label: "PWM", value: fan.on ? `${pwm}%` : "0%" },
    ],
    metadata: {
      status: { label: "Status", value: fan.on ? "Running" : "Off" },
    },
  };
}
