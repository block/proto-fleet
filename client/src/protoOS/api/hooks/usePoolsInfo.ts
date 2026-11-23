import { useCallback, useMemo, useRef, useState } from "react";

import { isActivePool } from "@/protoOS/api/adapters/poolStatistics";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetPoolsInfo, usePoolsInfo as useStoredPoolsInfo } from "@/protoOS/store";
import type { PoolInfo } from "@/protoOS/store/contract";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { useSetPoolsInfoError, useSetPoolsInfoPending } from "@/protoOS/store/hooks/usePools";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { usePoll } from "@/shared/hooks/usePoll";

export interface FetchPoolsInfoProps {
  onError?: (response?: string) => void;
  onSuccess?: (response?: PoolInfo[]) => void;
  retryOnMinerDown?: boolean;
  signal?: AbortSignal;
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
  const latestRequest = useRef(0);
  const data = useStoredPoolsInfo();
  const setPoolsInfo = useSetPoolsInfo();
  const setPoolsInfoPending = useSetPoolsInfoPending();
  const setPoolsInfoError = useSetPoolsInfoError();

  const fetchData = useCallback(
    ({ onSuccess, onError, retryOnMinerDown, signal }: FetchPoolsInfoProps = {}) => {
      if (!adapter) return;

      const performFetch = () => {
        if (signal?.aborted) return;
        const requestId = ++latestRequest.current;
        const accept = beginStoreWrite("pools:module:root");
        accept();
        setPending(true);
        setError(undefined);
        setPoolsInfoPending(true);
        return authRetry({
          request: (params) => adapter.getPools(params),
          signal,
          onSuccess: (sortedPools) => {
            if (signal?.aborted || !accept()) return;
            setPoolsInfo(sortedPools);
            onSuccess?.(sortedPools);
            if (retryOnMinerDown) {
              const noLivePools = !sortedPools?.some(isActivePool);
              // if all pools are dead, refetch pools
              if (noLivePools) {
                setTimeout(() => performFetch(), 5000);
              }
            }
          },
          onError: (e) => {
            if (signal?.aborted || !accept()) return;
            const newError = e?.error?.message ?? "An error occurred";
            if (retryOnMinerDown) {
              setTimeout(() => performFetch(), 5000);
            }
            setError(newError);
            setPoolsInfoError(newError);
            onError?.(newError);
          },
        }).finally(() => {
          // Another hook can supersede this request's shared store write, but
          // this caller must still leave its loading state when its work ends.
          if (requestId === latestRequest.current) setPending(false);
          if (!accept()) return;
          setPoolsInfoPending(false);
        });
      };

      return performFetch();
    },
    [adapter, authRetry, setPoolsInfo, setPoolsInfoError, setPoolsInfoPending],
  );

  usePoll({
    fetchData: (signal) => fetchData({ signal }),
    params: adapter,
    poll,
    pollIntervalMs,
  });

  return useMemo(() => ({ fetchData, pending, error, data }), [fetchData, pending, error, data]);
};

export { usePoolsInfo };
