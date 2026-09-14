import { useCallback, useMemo, useRef } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useSetSystemInfo,
  useSetSystemInfoError,
  useSetSystemInfoPending,
  useSystemInfoError,
  // TODO: currently have a naming conflict.  Rather than renaming this hook individually,
  // We should update all the API hooks to adopt a consistent naming scheme that would conflict, ie. useFetchSystemInfo
  useSystemInfo as useSystemInfoFromStore,
  useSystemInfoPending,
} from "@/protoOS/store";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import { usePoll } from "@/shared/hooks/usePoll";
import { isAbortError } from "@/shared/utils/abort";

interface UseSystemInfoProps {
  poll?: boolean;
  pollIntervalMs?: number;
}

/**
 * API hook for fetching system info.
 *
 * Manages fetching system info from the API and updates the centralized Zustand store.
 * Use this hook with polling in AppWrapper to keep system info up to date.
 *
 * For accessing system info data, use the store hooks directly:
 *   import { useSystemInfo, useIsProtoRig, etc. } from "@/protoOS/store";
 */

const useSystemInfo = ({ poll, pollIntervalMs }: UseSystemInfoProps) => {
  const { adapter } = useMinerHosting();
  const setSystemInfo = useSetSystemInfo();
  const setSystemInfoError = useSetSystemInfoError();
  const setSystemInfoPending = useSetSystemInfoPending();
  const data = useSystemInfoFromStore();
  const pending = useSystemInfoPending();
  const error = useSystemInfoError();
  const isFetchingRef = useRef<boolean>(false);

  const fetchData = useCallback(
    (signal?: AbortSignal) => {
      if (!adapter || isFetchingRef.current) {
        return;
      }

      isFetchingRef.current = true;
      const accept = beginStoreWrite("systemInfo");
      setSystemInfoPending(true);

      adapter
        .getSystemInfo({ signal })
        .then((systemInfo) => {
          if (!accept()) return;
          setSystemInfo(systemInfo);
          setSystemInfoPending(false);
        })
        .catch((err) => {
          // A cancel from navigation/unmount is teardown, not a fetch failure.
          if (isAbortError(err)) return;
          setSystemInfoError(err?.error?.message ?? "An error occurred");
        })
        .finally(() => {
          isFetchingRef.current = false;
        });
    },
    [adapter, setSystemInfo, setSystemInfoError, setSystemInfoPending],
  );

  const reload = useCallback(
    (signal?: AbortSignal) => {
      if (isFetchingRef.current) return;
      fetchData(signal);
    },
    [fetchData],
  );

  usePoll({
    fetchData: reload,
    poll,
    pollIntervalMs,
    enabled: !!adapter,
  });

  return useMemo(() => ({ pending, error, data, reload }), [pending, error, data, reload]);
};

export { useSystemInfo };
