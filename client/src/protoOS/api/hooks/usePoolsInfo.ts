import { useCallback, useEffect, useMemo, useState } from "react";

import { Pool } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetPoolsInfo } from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { usePoll } from "@/shared/hooks/usePoll";

export interface FetchPoolsInfoProps {
  onError?: (response?: string) => void;
  onSuccess?: (response?: Pool[]) => void;
  retryOnMinerDown?: boolean;
}

type UsePoolsInfoProps = {
  poll?: boolean;
  pollIntervalMs?: number;
};

const usePoolsInfo = ({ poll = false, pollIntervalMs }: UsePoolsInfoProps = {}) => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();
  const [data, setData] = useState<Pool[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const setPoolsInfo = useSetPoolsInfo();

  const fetchData = useCallback(
    ({ onSuccess, onError, retryOnMinerDown }: FetchPoolsInfoProps = {}) => {
      if (!api) return;

      const performFetch = () => {
        setPending(true);
        setError(undefined);
        authRetry({
          request: (params) => api.listPools(params),
          onSuccess: (res) => {
            // find the highest priority pool
            // highest priority is the lowest number
            const sortedPools = res?.data["pools"]?.sort((a, b) => (a.priority || 0) - (b.priority || 0));
            setData(sortedPools);
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
    [api, authRetry],
  );

  usePoll({
    fetchData,
    poll,
    pollIntervalMs,
  });

  // Update store whenever pools info changes
  useEffect(() => {
    setPoolsInfo(data);
  }, [data, setPoolsInfo]);

  return useMemo(() => ({ fetchData, pending, error, data }), [fetchData, pending, error, data]);
};

export { usePoolsInfo };
