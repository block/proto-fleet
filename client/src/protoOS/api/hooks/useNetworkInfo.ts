import { useCallback, useMemo, useRef } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useNetworkInfoError,
  useNetworkInfo as useNetworkInfoFromStore,
  useNetworkInfoPending,
  useSetNetworkInfo,
  useSetNetworkInfoError,
  useSetNetworkInfoPending,
} from "@/protoOS/store";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { usePoll } from "@/shared/hooks/usePoll";

interface UseNetworkInfoProps {
  enabled?: boolean;
  poll?: boolean;
  pollIntervalMs?: number;
}

/**
 * API hook for fetching network info.
 *
 * Manages fetching network info from the API and updates the centralized Zustand store.
 *
 * For accessing network info data, use the store hooks directly:
 *   import { useNetworkInfo, useIpAddress, useMacAddress, etc. } from "@/protoOS/store";
 */

const useNetworkInfo = ({ enabled = true, poll, pollIntervalMs }: UseNetworkInfoProps) => {
  const { api } = useMinerHosting();
  const authRetry = useAuthRetry();
  const setNetworkInfo = useSetNetworkInfo();
  const setNetworkInfoError = useSetNetworkInfoError();
  const setNetworkInfoPending = useSetNetworkInfoPending();
  const data = useNetworkInfoFromStore();
  const pending = useNetworkInfoPending();
  const error = useNetworkInfoError();
  const isFetchingRef = useRef<boolean>(false);

  const fetchData = useCallback(() => {
    if (!enabled || !api || isFetchingRef.current) return;

    isFetchingRef.current = true;
    setNetworkInfoPending(true);

    authRetry({
      request: (params) => api.getNetwork(params),
      onSuccess: (res) => {
        const responseData = res?.data["network-info"];
        setNetworkInfo(responseData);
        setNetworkInfoPending(false);
      },
      onError: (err) => setNetworkInfoError(err?.error?.message ?? "An error occurred"),
    }).finally(() => {
      isFetchingRef.current = false;
    });
  }, [api, enabled, authRetry, setNetworkInfo, setNetworkInfoError, setNetworkInfoPending]);

  const reload = useCallback(() => {
    if (isFetchingRef.current) return;
    fetchData();
  }, [fetchData]);

  usePoll({
    fetchData: reload,
    enabled,
    poll,
    pollIntervalMs,
  });

  return useMemo(() => ({ pending, error, data, reload }), [pending, error, data, reload]);
};

export { useNetworkInfo };
