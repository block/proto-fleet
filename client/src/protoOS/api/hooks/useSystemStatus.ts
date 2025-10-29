import { useCallback, useEffect, useMemo, useRef } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useSetSystemStatus,
  // Rename to avoid conflict with this hook's name
  useSystemStatus as useSystemStatusFromStore,
} from "@/protoOS/store";

/**
 * API hook for fetching system status.
 *
 * Manages fetching system status from the API and updates the centralized Zustand store.
 *
 * For accessing system status data, use the store hooks directly:
 *   import { useSystemStatus, useOnboarded, usePasswordSet, etc. } from "@/protoOS/store";
 */
const useSystemStatus = () => {
  const { api } = useMinerHosting();
  const setSystemStatus = useSetSystemStatus();
  const data = useSystemStatusFromStore();
  const hasFetchedRef = useRef<boolean>(false);

  useEffect(() => {
    if (!api || hasFetchedRef.current) return;

    api
      .getSystemStatus()
      .then((res) => {
        setSystemStatus({
          onboarded: res?.data.onboarded,
          passwordSet: res?.data.password_set,
        });
        hasFetchedRef.current = true;
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Error:", err);
        hasFetchedRef.current = true;
      });
  }, [api, setSystemStatus]);

  const reload = useCallback(() => {
    hasFetchedRef.current = false;
    if (!api) return;

    api
      .getSystemStatus()
      .then((res) => {
        setSystemStatus({
          onboarded: res?.data.onboarded,
          passwordSet: res?.data.password_set,
        });
        hasFetchedRef.current = true;
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Reload error:", err);
        hasFetchedRef.current = true;
      });
  }, [api, setSystemStatus]);

  return useMemo(() => ({ data, reload }), [data, reload]);
};

export { useSystemStatus };
