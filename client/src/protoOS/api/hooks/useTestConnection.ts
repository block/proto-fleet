import { useCallback, useMemo, useState } from "react";

import { ValidationMode } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { TestConnection } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { type PoolConnectionTestOutcome } from "@/shared/components/MiningPools/types";

export interface TestConnectionProps {
  onError?: () => void;
  onFinally?: () => void;
  onSuccess?: (outcome: PoolConnectionTestOutcome) => void;
  poolInfo: TestConnection;
}

const useTestConnection = () => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [pending, setPending] = useState(false);

  const testConnection = useCallback(
    ({ poolInfo, onSuccess, onError, onFinally }: TestConnectionProps) => {
      if (!api) return;

      setPending(true);
      authRetry({
        request: (params) => api.testPoolConnection(poolInfo, params),
        // The protoOS test endpoint runs a full SV1 subscribe + authorize
        // against the miner's configured pool, so a 200 means credentials
        // are verified end-to-end. Surface that to PoolModal as a
        // SV1_AUTHENTICATE outcome — same shape the fleet ValidatePool
        // returns for the SV1 success path.
        onSuccess: () =>
          onSuccess?.({
            reachable: true,
            credentialsVerified: true,
            mode: ValidationMode.SV1_AUTHENTICATE,
          }),
        onError: () => onError?.(),
      }).finally(() => {
        setPending(false);
        onFinally?.();
      });
    },
    [api, authRetry],
  );

  return useMemo(() => ({ pending, testConnection }), [pending, testConnection]);
};

export { useTestConnection };
