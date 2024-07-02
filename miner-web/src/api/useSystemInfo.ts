import { useEffect, useState } from "react";

import { api } from "./api";
import { SystemInfoSysteminfo } from "./types";

const useSystemInfo = () => {
  const [data, setData] = useState<SystemInfoSysteminfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    setPending(true);
    api
      .getSystemInfo()
      .then((res) => {
        setData(res?.data["system-info"]);
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

export { useSystemInfo };
