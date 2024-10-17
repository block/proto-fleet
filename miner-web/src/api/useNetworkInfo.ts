import { useEffect, useState } from "react";

import { api } from "./api";
import { NetworkInfoNetworkinfo } from "./types";

const useNetworkInfo = () => {
  const [data, setData] = useState<NetworkInfoNetworkinfo>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    setPending(true);
    api.getNetwork()
      .then((res) => {
        setData(res?.data["network-info"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
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

export { useNetworkInfo };
