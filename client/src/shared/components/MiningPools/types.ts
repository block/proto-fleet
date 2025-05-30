import { type info } from "./constants";

export type PoolInfo = Record<keyof typeof info, any>;

export type DefaultPoolIndex = 0;

export type BackupPoolIndex = 1 | 2;

export type PoolIndex = DefaultPoolIndex | BackupPoolIndex;

// Generic type for pool validation/test connection functions
export type PoolConnectionTestProps = {
  poolInfo: PoolInfo;
  onError?: (error?: string) => void;
  onSuccess?: () => void;
  onFinally?: () => void;
};
