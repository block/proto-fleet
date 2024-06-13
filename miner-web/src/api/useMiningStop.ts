import { useCallback, useState } from "react";

import { api } from "./api";
import { Error } from "./types";

const useMiningStop = () => {
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const stopMining = useCallback(() => {
    setPending(true);
    api
      .stopMining()
      .catch((err) => {
        setError(err?.error || { message: err });
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
