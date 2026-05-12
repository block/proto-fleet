export interface RestoreEstimate {
  batchCount: number;
  totalSeconds: number;
}

export function parsePositiveInteger(value: string): number | undefined {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
}

export function getRestoreEstimate({
  selectedCandidateCount,
  restoreBatchSize,
  restoreBatchIntervalSec,
}: {
  selectedCandidateCount: number;
  restoreBatchSize: string;
  restoreBatchIntervalSec: string;
}): RestoreEstimate | undefined {
  const batchSize = parsePositiveInteger(restoreBatchSize);
  const intervalSec = parsePositiveInteger(restoreBatchIntervalSec);

  if (batchSize === undefined || intervalSec === undefined || selectedCandidateCount <= 0) {
    return undefined;
  }

  const batchCount = Math.ceil(selectedCandidateCount / batchSize);

  return {
    batchCount,
    totalSeconds: Math.max(batchCount - 1, 0) * intervalSec,
  };
}

export function formatRestoreEstimate(estimate: RestoreEstimate): string {
  if (estimate.totalSeconds === 0) {
    return "Immediate";
  }

  const minutes = Math.floor(estimate.totalSeconds / 60);
  const seconds = estimate.totalSeconds % 60;

  if (minutes < 60) {
    if (seconds > 0) {
      return `${minutes}m ${seconds}s`;
    }

    return `${minutes}m`;
  }

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;

  if (remainingMinutes > 0) {
    return `${hours}h ${remainingMinutes}m`;
  }

  return `${hours}h`;
}
