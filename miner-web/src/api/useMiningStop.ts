import { useCallback, useState } from "react";

import { api } from "./api";

const useMiningStop = () => {
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const stopMining = useCallback(() => {
    setPending(true);
    api
      .stopMining()
      .catch((err) => {
        setError(err?.error?.message || err);
      })
      .finally(() => {
        setPending(false);
      });
  }, []);

  return {
    pending,
    error,
    stopMining,
  };
};

export { useMiningStop };
