import { useCallback, useState } from "react";

import { api } from "./api";

const useSystemReboot = () => {
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const rebootSystem = useCallback(() => {
    setPending(true);
    api
      .rebootSystem()
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
    rebootSystem,
  };
};

export { useSystemReboot };
