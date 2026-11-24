import type { PoolId } from "@/protoOS/store/contract";

export interface PoolHistoryRequest {
  poolIds: PoolId[];
  startTime: string;
  endTime: string;
  intervalMs: number;
}

export interface PoolShareStatistics {
  accepted?: number;
  rejected?: number;
  /** Fraction in [0, 1]; absent when no shares were observed. */
  acceptanceRate?: number;
  partial: boolean;
}

export interface PoolHistoryResult {
  byPool: Partial<Record<PoolId, PoolShareStatistics>>;
  unavailable: PoolId[];
}

/** The latest retained result, independent of the selected history period. */
export interface PoolRecordedShare {
  resultAtMs?: number;
  assignedDifficulty?: string;
  outcome: "accepted" | "rejected" | "submitError" | "unknown";
}

export type PoolRecordedShares = Partial<Record<PoolId, PoolRecordedShare>>;
