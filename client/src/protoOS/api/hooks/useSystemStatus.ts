import { useCallback, useMemo, useRef } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useDefaultPasswordActive,
  useOnboarded,
  usePasswordSet,
  useSetDefaultPasswordActive,
  useSetOnboarded,
  useSetPasswordSet,
} from "@/protoOS/store";
import { usePoll } from "@/shared/hooks/usePoll";

/**
 * API hook for fetching system status.
 *
 * Manages fetching system status from the API and updates the centralized Zustand store.
 *
 * For accessing system status data, use the store hooks directly:
 *   import { useOnboarded, usePasswordSet, useDefaultPasswordActive } from "@/protoOS/store";
 */
const useSystemStatus = () => {
  const { api } = useMinerHosting();
  const setOnboarded = useSetOnboarded();
  const setPasswordSet = useSetPasswordSet();
  const setDefaultPasswordActive = useSetDefaultPasswordActive();
  const onboarded = useOnboarded();
  const passwordSet = usePasswordSet();
  const defaultPasswordActive = useDefaultPasswordActive();
  const isFetchingRef = useRef(false);
  const hasLoadedStatus = onboarded !== undefined && passwordSet !== undefined;

  const data = useMemo(
    () => ({ onboarded, passwordSet, defaultPasswordActive }),
    [onboarded, passwordSet, defaultPasswordActive],
  );

  const fetchData = useCallback(() => {
    if (!api || isFetchingRef.current) return;

    isFetchingRef.current = true;
    return api
      .getSystemStatus({ secure: false })
      .then((res) => {
        setOnboarded(res?.data.onboarded);
        setPasswordSet(res?.data.password_set);
        setDefaultPasswordActive(res?.data.default_password_active);
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Error:", err);
      })
      .finally(() => {
        isFetchingRef.current = false;
      });
  }, [api, setOnboarded, setPasswordSet, setDefaultPasswordActive]);

  // Poll until initial status is loaded. Keep polling while defaultPasswordActive
  // is true so the store self-corrects after the user changes their password.
  usePoll({
    fetchData,
    poll: true,
    pollIntervalMs: 5000,
    enabled: !!api && (!hasLoadedStatus || defaultPasswordActive === true),
  });

  const reload = useCallback(() => {
    return fetchData();
  }, [fetchData]);

  return useMemo(() => ({ data, reload }), [data, reload]);
};

export { useSystemStatus };
