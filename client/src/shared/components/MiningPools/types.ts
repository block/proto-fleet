import { type poolInfoAttributes } from "./constants";
import { type ValidationMode } from "@/protoFleet/api/generated/pools/v1/pools_pb";

export type PoolInfo = Record<keyof typeof poolInfoAttributes, any>;

export type DefaultPoolIndex = 0;

export type BackupPoolIndex = 1 | 2;

export type PoolIndex = DefaultPoolIndex | BackupPoolIndex;

// PoolConnectionTestOutcome carries the typed probe result so consumers
// can render "reachable but credentials unverified" (the v1 SV2 default)
// without inferring it from string conventions.
export type PoolConnectionTestOutcome = {
  reachable: boolean;
  credentialsVerified: boolean;
  mode: ValidationMode;
};

// Generic type for pool validation/test connection functions
export type PoolConnectionTestProps = {
  poolInfo: PoolInfo;
  onError?: (error?: string) => void;
  onSuccess?: (outcome: PoolConnectionTestOutcome) => void;
  onFinally?: () => void;
};
