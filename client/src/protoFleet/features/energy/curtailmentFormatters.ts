import type { CurtailmentPriority } from "@/protoFleet/features/energy/types";

export const priorityLabels: Record<CurtailmentPriority, string> = {
  normal: "Normal",
  emergency: "Emergency",
};

export function formatKw(value: number, fractionDigits = 1): string {
  return `${value.toLocaleString(undefined, {
    maximumFractionDigits: fractionDigits,
    minimumFractionDigits: fractionDigits,
  })} kW`;
}
