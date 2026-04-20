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
        const nextDefaultPasswordActive = res?.data.default_password_active ?? false;

        // Resolve store state at response time — hook-scoped values are
        // captured at fire time and can be stale if the password-change
        // flow completed mid-flight.
        const currentDefaultPasswordActive = useMinerStore.getState().minerStatus.defaultPasswordActive;
        const currentAccessToken = useMinerStore.getState().auth.authTokens.accessToken;
        const hasValidSession = !!currentAccessToken?.value && new Date(currentAccessToken.expiry) > new Date();

        // Don't let polling clear the flag before follow-up login succeeds.
        // Once a valid session is established the clear is trustworthy —
        // gating only on the route leaves a trap when another client cleared
        // default_password_active server-side.
        const suppressClear =
          isPasswordChangeRoute &&
          currentDefaultPasswordActive === true &&
          nextDefaultPasswordActive === false &&
          !hasValidSession;

        // Don't let a stale response re-raise the flag after a valid session
        // has cleared it — the auth-error path owns re-entering the lockout.
        const suppressStaleRaise =
          nextDefaultPasswordActive === true && currentDefaultPasswordActive === false && hasValidSession;

        if (!suppressClear && !suppressStaleRaise) {
          setDefaultPasswordActive(nextDefaultPasswordActive);
        }
      })
      .catch((err) => {
        console.error("[useSystemStatus API hook] Error:", err);
      })
      .finally(() => {
        isFetchingRef.current = false;
      });
  }, [api, isPasswordChangeRoute, setOnboarded, setPasswordSet, setDefaultPasswordActive]);

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
