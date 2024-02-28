import { useState } from "react";

import { api } from "./api";
import { Error, ErrorResponse, Pool } from "./types";

interface FetchPoolsInfoProps {
  onError?: (response?: Error) => void;
  onSuccess?: (response?: Pool[]) => void;
}

const usePoolsInfo = () => {
  const [data, setData] = useState<Pool[]>();
  const [error, setError] = useState<Error>();
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
      .catch((err: ErrorResponse) => {
        setError(err?.error);
        onError?.(err?.error);
      })
      .finally(() => {
        setPending(false);
      });
  };

  return {
    fetch,
    pending,
    error,
    data,
  };
};

export { usePoolsInfo };
