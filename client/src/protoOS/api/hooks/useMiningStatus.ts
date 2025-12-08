import { useCallback, useEffect, useMemo, useState } from "react";

import { MiningStatusMiningstatus } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetMiningStatus } from "@/protoOS/store";
import { usePoll } from "@/shared/hooks/usePoll";

interface getMiningStatusProps {
  onSuccess?: (res?: MiningStatusMiningstatus) => void;
}

type UseMiningStatusProps = {
  poll?: boolean;
  pollIntervalMs?: number;
};

const useMiningStatus = ({ poll = false, pollIntervalMs }: UseMiningStatusProps = {}) => {
  const { api } = useMinerHosting();
  const [data, setData] = useState<MiningStatusMiningstatus>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const setMiningStatus = useSetMiningStatus();

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

  usePoll({
    fetchData,
    poll,
    pollIntervalMs,
  });

  // Update store whenever mining status changes
  useEffect(() => {
    if (data !== undefined) {
      setMiningStatus(data);
    }
  }, [data, setMiningStatus]);

  return useMemo(() => ({ fetchData, data, pending, error }), [fetchData, data, pending, error]);
};

export { useMiningStatus };
