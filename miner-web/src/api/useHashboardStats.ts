import { useCallback, useState } from "react";

import { api } from "./api";
import { HashboardStatsHashboardstats } from "./types";
import { usePoll } from "./usePoll";

interface UseHashboardStatsProps {
  hashboardSerialNumber: string;
  poll?: boolean;
}

const useHashboardStats = ({ hashboardSerialNumber, poll }: UseHashboardStatsProps) => {
  const [data, setData] = useState<HashboardStatsHashboardstats>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api.getHashboardStatus(hashboardSerialNumber)
      .then((res) => {
        setData(res?.data["hashboard-stats"]);
      })
      .catch((err) => {
        setError(err?.error?.message || err);
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
