import type { CurtailmentPriority, EditableCurtailmentPriority } from "@/protoFleet/api/curtailmentTypes";
import {
  type CurtailmentEvent,
  CurtailmentPriority as ProtoCurtailmentPriority,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

export const wattsPerKilowatt = 1000;
export const estimatedReductionKwSnapshotKeys = ["estimated_reduction_kw", "estimatedReductionKw"] as const;
export const selectedCountSnapshotKeys = ["selected_count", "selectedCount"] as const;

export function hasSnapshotNumber(event: CurtailmentEvent, keys: readonly string[]): boolean {
  return keys.some((key) => typeof event.decisionSnapshot?.[key] === "number");
}

type CurtailmentKwFormatOptions = {
  maximumFractionDigits?: number;
  minimumFractionDigits?: number;
  includeUnit?: boolean;
  normalizeNonFinite?: boolean;
};

export function formatCurtailmentKw(value: number, format: number | CurtailmentKwFormatOptions = 1): string {
  const options = typeof format === "number" ? { maximumFractionDigits: format } : format;
  const maximumFractionDigits = options.maximumFractionDigits ?? 1;
  const finiteValue = options.normalizeNonFinite !== false && !Number.isFinite(value) ? 0 : value;
  const magnitude = finiteValue.toLocaleString(undefined, {
    maximumFractionDigits,
    minimumFractionDigits: options.minimumFractionDigits ?? maximumFractionDigits,
  });
  return options.includeUnit === false ? magnitude : `${magnitude} kW`;
}

export function mapCurtailmentPriority(priority: ProtoCurtailmentPriority): CurtailmentPriority {
  switch (priority) {
    case ProtoCurtailmentPriority.EMERGENCY:
      return "emergency";
    case ProtoCurtailmentPriority.HIGH:
      return "high";
    default:
      return "normal";
  }
}

export function mapEditableCurtailmentPriority(priority: ProtoCurtailmentPriority): EditableCurtailmentPriority {
  const mapped = mapCurtailmentPriority(priority);
  return mapped === "high" ? "normal" : mapped;
}

export function toApiCurtailmentPriority(priority: EditableCurtailmentPriority): ProtoCurtailmentPriority {
  return priority === "emergency" ? ProtoCurtailmentPriority.EMERGENCY : ProtoCurtailmentPriority.NORMAL;
}

export function isCurtailmentAdmin(role: string): boolean {
  return role === "ADMIN" || role === "SUPER_ADMIN";
}
