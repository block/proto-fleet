import type { PoolInfo } from "@/shared/types/pool";

// Generic type for pool validation/test connection functions
export type PoolConnectionTestProps = {
  poolInfo: PoolInfo;
  onError?: (error?: string) => void;
  onSuccess?: () => void;
  onFinally?: () => void;
};
