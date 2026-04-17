import { useCallback, useMemo, useRef } from "react";
import { useLocation } from "react-router-dom";

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
  const location = useLocation();
  const onboarded = useOnboarded();
  const passwordSet = usePasswordSet();
  const defaultPasswordActive = useDefaultPasswordActive();
  const isFetchingRef = useRef(false);
  const hasLoadedStatus = onboarded !== undefined && passwordSet !== undefined;
  const isPasswordChangeRoute =
    location.pathname === "/onboarding/authentication" || location.pathname === "/settings/authentication";

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
        const nextDefaultPasswordActive = res?.data.default_password_active;

        // While the user is on a password-change route, do not let status
        // polling clear the flag before the follow-up login succeeds.
        if (!(isPasswordChangeRoute && defaultPasswordActive === true && nextDefaultPasswordActive === false)) {
          setDefaultPasswordActive(nextDefaultPasswordActive);
        }
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Error:", err);
      })
      .finally(() => {
        isFetchingRef.current = false;
      });
  }, [api, defaultPasswordActive, isPasswordChangeRoute, setOnboarded, setPasswordSet, setDefaultPasswordActive]);

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
