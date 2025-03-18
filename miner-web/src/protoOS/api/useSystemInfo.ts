import { useEffect, useMemo, useState } from "react";

import { SystemInfoSysteminfo } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const useSystemInfo = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<SystemInfoSysteminfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getSystemInfo()
      .then((res) => {
        setData(res?.data["system-info"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

  return useMemo(() => ({ pending, error, data }), [pending, error, data]);
};

export { useSystemInfo };
