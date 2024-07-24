import { useEffect, useState } from "react";

import { api } from "./api";
import { SystemStatuses } from "./types";

const useSystemStatus = () => {
  const [data, setData] = useState<SystemStatuses>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    setPending(true);
    api
      .getSystemStatus()
      .then((res) => {
        setData(res?.data);
      })
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
    data,
  };
};

export { useSystemStatus };
