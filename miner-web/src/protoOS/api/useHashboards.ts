import { useEffect, useState } from "react";

import { HashboardsInfoHashboardsinfo } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const useHashboards = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<HashboardsInfoHashboardsinfo[]>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  useEffect(() => {
    if (!api) return;

    setPending(true);
    api
      .getAllHashboards()
      .then((res) => {
        setData(res?.data["hashboards-info"]);
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

export { useHashboards };
