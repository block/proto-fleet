import { useCallback, useEffect, useMemo, useState } from "react";

import { usePoll } from "./usePoll";
import { Pool } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetPoolsInfo } from "@/protoOS/store";

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
        api
          .listPools()
          .then((res) => {
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
          })
          .catch((err) => {
            const newError = err?.error?.message ?? err;
            if (retryOnMinerDown) {
              setTimeout(() => performFetch(), 5000);
            }
            setError(newError);
            onError?.(newError);
          })
          .finally(() => {
            setPending(false);
          });
      };

      performFetch();
    },
    [api],
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
