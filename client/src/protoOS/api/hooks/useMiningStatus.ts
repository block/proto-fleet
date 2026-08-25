import { useCallback, useMemo, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetMiningStatus, useMiningStatus as useStoredMiningStatus } from "@/protoOS/store";
import type { OperationalStatus } from "@/protoOS/store/contract";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { usePoll } from "@/shared/hooks/usePoll";

interface GetMiningStatusProps {
  onSuccess?: (status?: OperationalStatus) => void;
}

type UseMiningStatusProps = {
  enabled?: boolean;
  poll?: boolean;
  pollIntervalMs?: number;
};

const useMiningStatus = ({ enabled = true, poll = false, pollIntervalMs }: UseMiningStatusProps = {}) => {
  const { adapter } = useMinerHosting();
  const { handleAuthErrors } = useAuthErrors();
  const data = useStoredMiningStatus();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);
  const setMiningStatus = useSetMiningStatus();

  const fetchData = useCallback(
    ({ onSuccess }: GetMiningStatusProps = {}) => {
      if (!enabled || !adapter) return;

      setPending(true);
      const accept = beginStoreWrite("operationalStatus:module:root");
      setError(undefined);
      return adapter
        .getMiningStatus()
        .then((status) => {
          if (!accept()) return;
          setMiningStatus(status);
          onSuccess?.(status);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: (response) => setError(response?.error?.message ?? "An error occurred"),
          });
        })
        .finally(() => setPending(false));
    },
    [adapter, enabled, handleAuthErrors, setMiningStatus],
  );

  usePoll({ fetchData, enabled, poll, pollIntervalMs });

  return useMemo(() => ({ fetchData, data, pending, error }), [fetchData, data, pending, error]);
};

export { useMiningStatus };
