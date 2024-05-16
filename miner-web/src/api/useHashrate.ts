import { useCallback, useEffect, useState } from "react";

import { api } from "./api";
import { HashrateResponseHashratedata } from "./types";

interface UseHashrateProps {
  duration: HashrateResponseHashratedata["duration"];
  poll?: boolean;
}

const useHashrate = ({ duration, poll }: UseHashrateProps) => {
  const [data, setData] = useState<HashrateResponseHashratedata>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    setPending(true);
    api
      .getMinerHashrate({ duration })
      .then((res) => {
        setData(res?.data["hashrate-data"]);
      })
      .catch((err) => {
        setError(err?.error);
      })
      .finally(() => {
        setPending(false);
      });
  }, [duration]);

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

export { useHashrate };
