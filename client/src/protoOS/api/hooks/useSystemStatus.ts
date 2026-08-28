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
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
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
  const { adapter } = useMinerHosting();
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
    if (!adapter || isFetchingRef.current) return;

    isFetchingRef.current = true;
    const accept = beginStoreWrite("deviceLifecycle");
    return adapter
      .getSystemStatus()
      .then((status) => {
        if (!accept()) return;
        setOnboarded(status.onboarded);
        setPasswordSet(status.passwordSet);

        // MDK-API 1.8.2 removed default_password_active from system status, so
        // polling can no longer confirm or clear the flag. It is raised by the
        // 403 default-password contract (useAuthErrors) and cleared by the
        // password-change flow. Resolve only the initial undefined state so
        // App.tsx's protected-API gating can proceed. Read the store at
        // response time — the 403 path may have raised the flag mid-flight.
        if (useMinerStore.getState().minerStatus.lifecycle.defaultPasswordActive === undefined) {
          setDefaultPasswordActive(false);
        }
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Error:", err);
      })
      .finally(() => {
        isFetchingRef.current = false;
      });
  }, [adapter, setOnboarded, setPasswordSet, setDefaultPasswordActive]);

  // Poll until initial status is loaded.
  usePoll({
    fetchData,
    poll: true,
    pollIntervalMs: 5000,
    enabled: !!adapter && !hasLoadedStatus,
  });

  const reload = useCallback(() => {
    return fetchData();
  }, [fetchData]);

  return useMemo(() => ({ data, reload }), [data, reload]);
};

export { useSystemStatus };
