import { useCallback, useState } from "react";

import { api } from "./api";
import { PoolConfig } from "./types";

const useCreatePool = () => {
  const [error, setError] = useState();
  const [pending, setPending] = useState<boolean>(false);

  const createPool = useCallback(async (poolInfo: PoolConfig) => {
    setPending(true);
    api.createPool(poolInfo)
      .catch((err) => {
        setError(err?.error);
      })
      .finally(() => {
        setPending(false);
      });
  }, []);

  return {
    createPool,
    pending,
    setPending,
    error,
  };
};

export { useCreatePool };
