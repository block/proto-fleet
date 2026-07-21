import { CohortConfigDimension, type CohortSummary } from "@/protoFleet/api/generated/cohort/v1/cohort_pb";

export type ConvergenceSegment = {
  label: string;
  count: number;
  color: string;
};

export const firmwareConvergenceSegments = (cohort: CohortSummary): ConvergenceSegment[] => [
  {
    label: "Complete",
    count: cohort.firmwareProgress?.completeCount ?? 0,
    color: "var(--color-intent-success-fill)",
  },
  {
    label: "In progress",
    count:
      (cohort.firmwareProgress?.queuedCount ?? 0) +
      (cohort.firmwareProgress?.updatingCount ?? 0) +
      (cohort.firmwareProgress?.verifyingCount ?? 0),
    color: "var(--color-core-accent-fill)",
  },
  {
    label: "Needs attention",
    count: cohort.firmwareProgress?.needsAttentionCount ?? 0,
    color: "var(--color-intent-critical-fill)",
  },
  {
    label: "Unknown",
    count: cohort.firmwareProgress?.unknownCount ?? 0,
    color: "var(--color-core-primary-20)",
  },
];

export const poolConvergenceSegments = (cohort: CohortSummary): ConvergenceSegment[] => {
  const progress = cohort.configProgress.find((item) => item.dimension === CohortConfigDimension.POOLS);
  return [
    {
      label: "Converged",
      count: progress?.convergedCount ?? 0,
      color: "var(--color-intent-success-fill)",
    },
    {
      label: "In progress",
      count: (progress?.waitingCount ?? 0) + (progress?.applyingCount ?? 0) + (progress?.verifyingCount ?? 0),
      color: "var(--color-core-accent-fill)",
    },
    { label: "Held", count: progress?.heldCount ?? 0, color: "var(--color-intent-warning-fill)" },
    { label: "Failed", count: progress?.failedCount ?? 0, color: "var(--color-intent-critical-fill)" },
    { label: "Unsupported", count: progress?.unsupportedCount ?? 0, color: "var(--color-core-primary-20)" },
  ];
};
