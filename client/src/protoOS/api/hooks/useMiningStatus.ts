import { useCallback, useEffect, useMemo, useState } from "react";

import { MiningStatusMiningstatus } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetMiningStatus } from "@/protoOS/store";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { usePoll } from "@/shared/hooks/usePoll";

interface getMiningStatusProps {
  onSuccess?: (res?: MiningStatusMiningstatus) => void;
}

type UseMiningStatusProps = {
  enabled?: boolean;
  poll?: boolean;
  pollIntervalMs?: number;
};

const useMiningStatus = ({ enabled = true, poll = false, pollIntervalMs }: UseMiningStatusProps = {}) => {
  const { api } = useMinerHosting();
  const { handleAuthErrors } = useAuthErrors();
  const [data, setData] = useState<MiningStatusMiningstatus>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<boolean>(false);
  const setMiningStatus = useSetMiningStatus();

  const fetchData = useCallback(
    ({ onSuccess }: getMiningStatusProps = {}) => {
      if (!enabled || !api) return;

      setPending(true);
      api
        .getMiningStatus()
        .then((res) => {
          setData(res?.data["mining-status"]);
          onSuccess?.(res?.data["mining-status"]);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: (e) => setError(e?.error?.message ?? "An error occurred"),
          });
        })
        .finally(() => {
          setPending(false);
        });
    },
    [api, enabled, handleAuthErrors],
  );

  usePoll({
    fetchData,
    enabled,
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
