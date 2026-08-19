import { useCallback, useMemo, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetPoolsInfo, usePoolsInfo as useStoredPoolsInfo } from "@/protoOS/store";
import type { PoolInfo } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { usePoll } from "@/shared/hooks/usePoll";

export interface FetchPoolsInfoProps {
  onError?: (response?: string) => void;
  onSuccess?: (response?: PoolInfo[]) => void;
  retryOnMinerDown?: boolean;
}

type UsePoolsInfoProps = {
  poll?: boolean;
  pollIntervalMs?: number;
};

const usePoolsInfo = ({ poll = false, pollIntervalMs }: UsePoolsInfoProps = {}) => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const data = useStoredPoolsInfo();
  const setPoolsInfo = useSetPoolsInfo();

  const fetchData = useCallback(
    ({ onSuccess, onError, retryOnMinerDown }: FetchPoolsInfoProps = {}) => {
      if (!adapter) return;

      const performFetch = () => {
        setPending(true);
        setError(undefined);
        authRetry({
          request: (params) => adapter.getPools(params),
          onSuccess: (sortedPools) => {
            setPoolsInfo(sortedPools);
            onSuccess?.(sortedPools);
            if (retryOnMinerDown) {
              // TODO: remove alive when cgminer is removed
              const noLivePools = !sortedPools?.find((pool) => /alive|active/i.test(pool?.status ?? ""));
              // if all pools are dead, refetch pools
              if (noLivePools) {
                setTimeout(() => performFetch(), 5000);
              }
            }
          },
          onError: (e) => {
            const newError = e?.error?.message ?? "An error occurred";
            if (retryOnMinerDown) {
              setTimeout(() => performFetch(), 5000);
            }
            setError(newError);
            onError?.(newError);
          },
        }).finally(() => {
          setPending(false);
        });
      };

      performFetch();
    },
    [adapter, authRetry, setPoolsInfo],
  );

  usePoll({
    fetchData,
    poll,
    pollIntervalMs,
  });

  return useMemo(() => ({ fetchData, pending, error, data }), [fetchData, pending, error, data]);
};

export { usePoolsInfo };
