import { useCallback, useState } from "react";

import { api } from "./api";
import { Pool } from "./types";

export interface FetchPoolsInfoProps {
  onError?: (response?: string) => void;
  onSuccess?: (response?: Pool[]) => void;
  retryOnMinerDown?: boolean;
}

const usePoolsInfo = () => {
  const [data, setData] = useState<Pool[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(
    ({ onSuccess, onError, retryOnMinerDown }: FetchPoolsInfoProps = {}) => {
      setPending(true);
      setData(undefined);
      setError(undefined);
      api
        .listPools()
        .then((res) => {
          // find the highest priority pool
          // highest priority is the lowest number
          const sortedPools = res?.data["pools"]?.sort(
            (a, b) => (a.priority || 0) - (b.priority || 0)
          );
          setData(sortedPools);
          onSuccess?.(sortedPools);
        })
        .catch((err) => {
          const newError = err?.error?.message || err;
          if (retryOnMinerDown) {
            // refetch pools until cgminer comes back up
            if (/failed to connect to cgminer/i.test(newError)) {
              setTimeout(
                () => fetchData({ onSuccess, onError, retryOnMinerDown }),
                5000
              );
            }
          }
          setError(newError);
          onError?.(newError);
        })
        .finally(() => {
          setPending(false);
        });
    },
    []
  );

  return {
    fetchData,
    pending,
    error,
    data,
  };
};

export { usePoolsInfo };
