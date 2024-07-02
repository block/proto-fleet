import { useCallback, useState } from "react";

import { api } from "./api";

const useMiningStart = () => {
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const startMining = useCallback(() => {
    setPending(true);
    api
      .startMining()
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
    startMining,
  };
};

export { useMiningStart };
