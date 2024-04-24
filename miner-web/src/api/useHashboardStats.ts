import { useEffect, useState } from "react";

import { api } from "./api";
import { HashboardStatsHashboardstats } from "./types";

const useHashboardStats = (hashboardSerialNumber: string) => {
  const [data, setData] = useState<HashboardStatsHashboardstats>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    setPending(true);
    api.getHashboardStatus(hashboardSerialNumber)
      .then((res) => {
        setData(res?.data["hashboard-stats"]);
      })
      .catch((err) => {
        setError(err?.error);
      })
      .finally(() => {
        setPending(false);
      });
  }, [hashboardSerialNumber]);

  return {
    pending,
    error,
    data,
  };
};

export { useHashboardStats };
