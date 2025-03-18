import { useEffect, useMemo, useState } from "react";

import { NetworkInfoNetworkinfo } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const useNetworkInfo = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<NetworkInfoNetworkinfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getNetwork()
      .then((res) => {
        setData(res?.data["network-info"]);
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

export { useNetworkInfo };
