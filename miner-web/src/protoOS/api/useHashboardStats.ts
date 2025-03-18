import { useCallback, useMemo, useState } from "react";

import { HashboardStatsHashboardstats } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseHashboardStatsProps {
  hashboardSerialNumber: string;
  poll?: boolean;
}

const useHashboardStats = ({
  hashboardSerialNumber,
  poll,
}: UseHashboardStatsProps) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HashboardStatsHashboardstats>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getHashboardStatus(hashboardSerialNumber)
      .then((res) => {
        setData(res?.data["hashboard-stats"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [hashboardSerialNumber, api]);

  usePoll({
    fetchData,
    params: hashboardSerialNumber,
    poll,
  });

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useHashboardStats };
