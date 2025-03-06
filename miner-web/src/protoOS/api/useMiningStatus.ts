import { useCallback, useState } from "react";

import { MiningStatusMiningstatus } from "@/protoOS/api/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface getMiningStatusProps {
  onSuccess?: (res?: MiningStatusMiningstatus) => void;
}

const useMiningStatus = () => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<MiningStatusMiningstatus>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);

  const fetchData = useCallback(
    ({ onSuccess }: getMiningStatusProps = {}) => {
      if (!api) return;

      setPending(true);
      api
        .getMiningStatus()
        .then((res) => {
          setData(res?.data["mining-status"]);
          onSuccess?.(res?.data["mining-status"]);
        })
        .catch((err) => {
          setError(err?.error?.message ?? err);
        })
        .finally(() => {
          setPending(false);
        });
    },
    [api],
  );

  return {
    fetchData,
    data,
    pending,
    error,
  };
};

export { useMiningStatus };
