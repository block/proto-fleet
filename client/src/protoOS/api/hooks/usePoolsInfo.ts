import { useCallback, useMemo, useState } from "react";

import { Pool } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

export interface FetchPoolsInfoProps {
  onError?: (response?: string) => void;
  onSuccess?: (response?: Pool[]) => void;
  retryOnMinerDown?: boolean;
}

const usePoolsInfo = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<Pool[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(
    ({ onSuccess, onError, retryOnMinerDown }: FetchPoolsInfoProps = {}) => {
      if (!api) return;

      setPending(true);
      setError(undefined);
      api
        .listPools()
        .then((res) => {
          // find the highest priority pool
          // highest priority is the lowest number
          const sortedPools = res?.data["pools"]?.sort(
            (a, b) => (a.priority || 0) - (b.priority || 0),
          );
          setData(sortedPools);
          onSuccess?.(sortedPools);
          if (retryOnMinerDown) {
            // TODO: remove alive when cgminer is removed
            const noLivePools = !sortedPools?.find((pool) =>
              /alive|active/i.test(pool?.status ?? ""),
            );
            // if all pools are dead, refetch pools
            if (noLivePools) {
              setTimeout(
                () => fetchData({ onSuccess, onError, retryOnMinerDown }),
                5000,
              );
            }
          }
        })
        .catch((err) => {
          const newError = err?.error?.message ?? err;
          if (retryOnMinerDown) {
            setTimeout(
              () => fetchData({ onSuccess, onError, retryOnMinerDown }),
              5000,
            );
          }
          setError(newError);
          onError?.(newError);
        })
        .finally(() => {
          setPending(false);
        });
    },
    [api],
  );

  return useMemo(
    () => ({ fetchData, pending, error, data }),
    [fetchData, pending, error, data],
  );
};

export { usePoolsInfo };
