import type { ModuleStatus } from "./tankView";

export const statusColor: Record<ModuleStatus, string> = {
  attention: "bg-intent-critical-fill",
  offline: "bg-core-accent-fill",
  isolated: "bg-text-primary-30",
  sleeping: "bg-text-primary-30",
  healthy: "bg-text-primary",
  unknown: "border border-border-30",
};

const attentionFillByIssueCount: Record<number, string> = {
  1: "bg-intent-critical-fill/20",
  2: "bg-intent-critical-fill/50",
};

export const hashboardAttentionFill = (issueCount: number | null | undefined) =>
  attentionFillByIssueCount[issueCount ?? 0] ?? "bg-intent-critical-fill";
