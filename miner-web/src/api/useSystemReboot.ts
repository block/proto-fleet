import { useCallback, useState } from "react";

import { api } from "./api";
import { Error } from "./types";

const useSystemReboot = () => {
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  const rebootSystem = useCallback(() => {
    setPending(true);
    api
      .rebootSystem()
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
    rebootSystem,
  };
};

export { useSystemReboot };
