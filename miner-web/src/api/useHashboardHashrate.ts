import { useCallback, useEffect, useState } from "react";

import { api } from "./api";
import { HashrateResponseHashratedata } from "./types";

interface UseHashboardHashrateProps {
  duration: HashrateResponseHashratedata["duration"];
  hashboardSerial: string;
  poll?: boolean;
}

const useHashboardHashrate = ({
  duration,
  hashboardSerial,
  poll,
}: UseHashboardHashrateProps) => {
  const [data, setData] = useState<HashrateResponseHashratedata>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!hashboardSerial) return;
    setPending(true);
    api
      .getHashboardHashrate(hashboardSerial, { duration })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
      })
      .catch((err) => {
        setError(err?.error);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration, hashboardSerial]);

  useEffect(() => {
    fetchData();
    if (poll) {
      const interval = setInterval(fetchData, 60000);
      return () => clearInterval(interval);
    }
  }, [fetchData, poll]);

  return {
    pending,
    error,
    data,
  };
};

export { useHashboardHashrate };
