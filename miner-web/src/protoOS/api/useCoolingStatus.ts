import { useCallback, useState } from "react";

import { CoolingStatusCoolingstatus } from "./types";
import { usePoll } from "./usePoll";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface UseCoolingStatusProps {
  poll?: boolean;
}

const useCoolingStatus = ({ poll }: UseCoolingStatusProps = {}) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<CoolingStatusCoolingstatus>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api) return;

    setPending(true);
    api
      .getCooling()
      .then((res) => {
        setData(res?.data["cooling-status"]);
      })
      .catch((err) => {
        setError(err?.error?.message ?? err);
      })
      .finally(() => {
        setPending(false);
      });
  }, [api]);

  usePoll({
    fetchData,
    poll,
  });

  return {
    pending,
    error,
    data,
  };
};

export { useCoolingStatus };
