import { useEffect, useState } from "react";

import { api } from "./api";
import { Error, SystemInfoSysteminfo } from "./types";

const useSystemInfo = () => {
  const [data, setData] = useState<SystemInfoSysteminfo>();
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    setPending(true);
    api
      .getSystemInfo()
      .then((res) => {
        setData(res?.data["system-info"]);
      })
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
    data,
  };
};

export { useSystemInfo };
