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
