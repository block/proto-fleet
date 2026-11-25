const attentionFillByIssueCount: Record<number, string> = {
  1: "bg-intent-critical-fill/20",
  2: "bg-intent-critical-fill/50",
};

export const hashboardAttentionFill = (issueCount: number | null | undefined) =>
  attentionFillByIssueCount[issueCount ?? 0] ?? "bg-intent-critical-fill";
