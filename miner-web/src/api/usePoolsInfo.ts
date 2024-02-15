import { useEffect, useState } from "react";

import { api } from "./api";
import { Pool } from "./types";

const usePoolsInfo = () => {
  const [data, setData] = useState<Pool[]>();
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
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
      })
      .catch((err) => {
        setError(err?.error);
      })
      .finally(() => {
        setPending(false);
      });
  }, []);

  return {
    pending,
    error,
    data,
  };
};

export { usePoolsInfo };
