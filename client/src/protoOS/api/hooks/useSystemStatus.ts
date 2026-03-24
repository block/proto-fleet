import { useCallback, useMemo, useRef } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useOnboarded, usePasswordSet, useSetOnboarded, useSetPasswordSet } from "@/protoOS/store";
import { usePoll } from "@/shared/hooks/usePoll";

/**
 * API hook for fetching system status.
 *
 * Manages fetching system status from the API and updates the centralized Zustand store.
 *
 * For accessing system status data, use the store hooks directly:
 *   import { useOnboarded, usePasswordSet } from "@/protoOS/store";
 */
const useSystemStatus = () => {
  const { api } = useMinerHosting();
  const setOnboarded = useSetOnboarded();
  const setPasswordSet = useSetPasswordSet();
  const onboarded = useOnboarded();
  const passwordSet = usePasswordSet();
  const isFetchingRef = useRef(false);
  const hasLoadedStatus = onboarded !== undefined && passwordSet !== undefined;

  const data = useMemo(() => ({ onboarded, passwordSet }), [onboarded, passwordSet]);

  const fetchData = useCallback(() => {
    if (!api || isFetchingRef.current) return;

    isFetchingRef.current = true;
    return api
      .getSystemStatus()
      .then((res) => {
        setOnboarded(res?.data.onboarded);
        setPasswordSet(res?.data.password_set);
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Error:", err);
      })
      .finally(() => {
        isFetchingRef.current = false;
      });
  }, [api, setOnboarded, setPasswordSet]);

  usePoll({
    fetchData,
    poll: true,
    pollIntervalMs: 5000,
    enabled: !!api && !hasLoadedStatus,
  });

  const reload = useCallback(() => {
    return fetchData();
  }, [fetchData]);

  return useMemo(() => ({ data, reload }), [data, reload]);
};

export { useSystemStatus };
