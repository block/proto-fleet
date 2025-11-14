import { useCallback, useEffect, useMemo, useRef } from "react";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useOnboarded,
  usePasswordSet,
  useSetOnboarded,
  useSetPasswordSet,
} from "@/protoOS/store";

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
  const hasFetchedRef = useRef<boolean>(false);

  const data = useMemo(
    () => ({ onboarded, passwordSet }),
    [onboarded, passwordSet],
  );

  useEffect(() => {
    if (!api || hasFetchedRef.current) return;

    api
      .getSystemStatus()
      .then((res) => {
        setOnboarded(res?.data.onboarded);
        setPasswordSet(res?.data.password_set);
        hasFetchedRef.current = true;
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Error:", err);
        hasFetchedRef.current = true;
      });
  }, [api, setOnboarded, setPasswordSet]);

  const reload = useCallback(() => {
    hasFetchedRef.current = false;
    if (!api) return;

    api
      .getSystemStatus()
      .then((res) => {
        setOnboarded(res?.data.onboarded);
        setPasswordSet(res?.data.password_set);
        hasFetchedRef.current = true;
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Reload error:", err);
        hasFetchedRef.current = true;
      });
  }, [api, setOnboarded, setPasswordSet]);

  return useMemo(() => ({ data, reload }), [data, reload]);
};

export { useSystemStatus };
