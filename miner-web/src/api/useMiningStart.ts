import { useCallback, useState } from "react";

import { api } from "./api";
import { Error } from "./types";

const useMiningStart = () => {
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const startMining = useCallback(() => {
    setPending(true);
    api
      .startMining()
      .catch((err) => {
        setError(err?.error || err);
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
