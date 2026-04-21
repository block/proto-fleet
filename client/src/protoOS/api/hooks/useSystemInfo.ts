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
import { usePoll } from "@/shared/hooks/usePoll";

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
  const { api } = useMinerHosting();
  const setSystemInfo = useSetSystemInfo();
  const setSystemInfoError = useSetSystemInfoError();
  const setSystemInfoPending = useSetSystemInfoPending();
  const data = useSystemInfoFromStore();
  const pending = useSystemInfoPending();
  const error = useSystemInfoError();
  const isFetchingRef = useRef<boolean>(false);

  const fetchData = useCallback(() => {
    if (!api || isFetchingRef.current) {
      return;
    }

    isFetchingRef.current = true;
    setSystemInfoPending(true);

    api
      .getSystemInfo()
      .then((res) => {
        const responseData = res?.data["system-info"];
        setSystemInfo(responseData);
        setSystemInfoPending(false);
      })
      .catch((err) => {
        setSystemInfoError(err?.error?.message ?? "An error occurred");
      })
      .finally(() => {
        isFetchingRef.current = false;
      });
  }, [api, setSystemInfo, setSystemInfoError, setSystemInfoPending]);

  const reload = useCallback(() => {
    if (isFetchingRef.current) return;
    fetchData();
  }, [fetchData]);

  usePoll({
    fetchData: reload,
    poll,
    pollIntervalMs,
  });

  return useMemo(() => ({ pending, error, data, reload }), [pending, error, data, reload]);
};

export { useSystemInfo };
