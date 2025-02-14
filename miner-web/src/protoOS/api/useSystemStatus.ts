import { useEffect, useState } from "react";

import { SystemStatuses } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const useSystemStatus = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<SystemStatuses>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getSystemStatus()
      .then((res) => {
        setData(res?.data);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

  return {
    pending,
    error,
    data,
  };
};

export { useSystemStatus };
