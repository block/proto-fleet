import { useEffect, useState } from "react";

import { api } from "./api";
import { Pool } from "./types";

interface FetchPoolsInfoProps {
  onError?: (response?: string) => void;
  onSuccess?: (response?: Pool[]) => void;
}

const usePoolsInfo = (shouldFetch?: boolean) => {
  const [data, setData] = useState<Pool[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetch = ({ onSuccess, onError }: FetchPoolsInfoProps = {}) => {
    setPending(true);
    api
      .listPools()
      .then((res) => {
        // find the highest priority pool that is alive
        // highest priority is the lowest number
        const sortedPools = res?.data["pools"]?.sort(
          (a, b) => (a.priority || 0) - (b.priority || 0)
        );
        setData(sortedPools);
        onSuccess?.(sortedPools);
      })
      .catch((err) => {
        const newError = err?.error?.message || err;
        setError(newError);
        onError?.(newError);
      })
      .finally(() => {
        setPending(false);
      });
  };

  useEffect(() => {
    if (shouldFetch) {
      fetch();
    }
  }, [shouldFetch]);

  return {
    fetch,
    pending,
    error,
    data,
  };
};

export { usePoolsInfo };
