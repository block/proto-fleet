import { useCallback, useMemo, useState } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSetErrors } from "@/protoOS/store";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { usePoll } from "@/shared/hooks/usePoll";
import { isAbortError } from "@/shared/utils/abort";

type UseErrorsProps = {
  poll?: boolean;
  pollIntervalMs?: number;
};

const useErrors = ({ poll = false, pollIntervalMs }: UseErrorsProps = {}) => {
  const { adapter } = useMinerHosting();
  const { handleAuthErrors } = useAuthErrors();
  const data = useMinerStore((state) => state.minerStatus.errorsByOwner.get(ROOT_MODULE_KEY));
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);
  const setErrors = useSetErrors();

  const fetchData = useCallback(
    (signal: AbortSignal) => {
      if (!adapter) return;

      setPending(true);
      const accept = beginStoreWrite("errors:module:root");
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

  usePoll({ fetchData, poll, pollIntervalMs });

  return useMemo(() => ({ fetchData, pending, error, data }), [fetchData, pending, error, data]);
};

export { useErrors };
