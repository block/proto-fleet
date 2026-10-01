import { useCallback, useMemo, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetErrors } from "@/protoOS/store";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { usePoll } from "@/shared/hooks/usePoll";
import { isAbortError } from "@/shared/utils/abort";

type UseErrorsProps = {
  enabled?: boolean;
  poll?: boolean;
  pollIntervalMs?: number;
};

const useErrors = ({ enabled = true, poll = false, pollIntervalMs }: UseErrorsProps = {}) => {
  const { adapter } = useMinerHosting();
  const { handleAuthErrors } = useAuthErrors();
  const byOwner = useMinerStore((state) => state.minerStatus.errorsByOwner);
  const data = useMemo(() => [...byOwner.values()].flat(), [byOwner]);
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);
  const setErrors = useSetErrors();

  const fetchData = useCallback(
    (signal: AbortSignal) => {
      if (!adapter) return;

      setPending(true);
      const accept = beginStoreWrite("errors:current");
      setError(undefined);
      return adapter
        .getErrors({ signal })
        .then((errors) => {
          if (accept()) setErrors(errors);
        })
        .catch((err) => {
          // A cancel from navigation/unmount is teardown, not an auth failure.
          if (isAbortError(err)) return;
          handleAuthErrors({
            error: err,
            onError: (response) => setError(response?.error?.message ?? "An error occurred"),
          });
        })
        .finally(() => setPending(false));
    },
    [adapter, handleAuthErrors, setErrors],
  );

  // Start the initial fetch once device discovery has supplied an adapter.
  usePoll({ fetchData, enabled: enabled && Boolean(adapter), poll, pollIntervalMs });

  return useMemo(() => ({ fetchData, pending, error, data }), [fetchData, pending, error, data]);
};

export { useErrors };
