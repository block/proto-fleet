import { type poolInfoAttributes } from "./constants";

export type PoolInfo = Record<keyof typeof poolInfoAttributes, any> & {
  v2_authority_pubkey?: string;
};

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
