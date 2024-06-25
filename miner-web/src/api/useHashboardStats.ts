import { useCallback, useState } from "react";

import { api } from "./api";
import { Error, HashboardStatsHashboardstats } from "./types";
import { usePoll } from "./usePoll";

interface UseHashboardStatsProps {
  hashboardSerialNumber: string;
  poll?: boolean;
}

const useHashboardStats = ({ hashboardSerialNumber, poll }: UseHashboardStatsProps) => {
  const [data, setData] = useState<HashboardStatsHashboardstats>();
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api.getHashboardStatus(hashboardSerialNumber)
      .then((res) => {
        setData(res?.data["hashboard-stats"]);
      })
      .catch((err) => {
        setError(err?.error || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [hashboardSerialNumber]);

  usePoll({ fetchData, poll });

  return {
    pending,
    error,
    data,
  };
};

export { useHashboardStats };
