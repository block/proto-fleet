import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { instanceUpdateClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { isAuthOrPermissionError, isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import { UPDATE_STATUS_INVALIDATED_EVENT } from "@/protoFleet/api/updateStatusEvents";
import {
  useAuthErrors,
  useFleetStore,
  useHasPermission,
  useIsAuthenticated,
  usePermissions,
  useSessionExpiry,
  useSetPermissions,
} from "@/protoFleet/store";
import { usePoll } from "@/shared/hooks/usePoll";

// The server refreshes release discovery roughly hourly; poll this status far
// slower than the telemetry-cadence POLL_INTERVAL_MS used elsewhere.
const UPDATE_STATUS_POLL_INTERVAL_MS = 15 * 60 * 1000;
const INSTANCE_UPDATE_PERMISSION = "instance:update";

export interface UseUpdateStatusResult {
  status: GetUpdateStatusResponse | null;
  hasUpdatePermission: boolean;
}

const statusUnchanged = (prev: GetUpdateStatusResponse, next: GetUpdateStatusResponse) =>
  prev.currentVersion === next.currentVersion &&
  prev.statusAvailable === next.statusAvailable &&
  prev.updateAvailable === next.updateAvailable &&
  prev.channel === next.channel &&
  prev.installCommand === next.installCommand &&
  prev.latestEligible?.version === next.latestEligible?.version;

interface AuthBoundarySnapshot {
  isAuthenticated: boolean;
  sessionExpiry: Date | null;
  permissions: string[];
}

interface CachedUpdateStatus {
  response: GetUpdateStatusResponse;
  authBoundary: AuthBoundarySnapshot;
}

const captureAuthBoundary = (): AuthBoundarySnapshot => {
  const { isAuthenticated, sessionExpiry, permissions } = useFleetStore.getState().auth;
  return { isAuthenticated, sessionExpiry, permissions };
};

const authBoundariesMatch = (left: AuthBoundarySnapshot, right: AuthBoundarySnapshot) =>
  left.isAuthenticated === right.isAuthenticated &&
  left.sessionExpiry === right.sessionExpiry &&
  left.permissions === right.permissions;

const isCurrentAuthBoundary = (snapshot: AuthBoundarySnapshot) => {
  return authBoundariesMatch(snapshot, captureAuthBoundary());
};

const currentlyHasUpdatePermission = () =>
  useFleetStore.getState().auth.permissions.includes(INSTANCE_UPDATE_PERMISSION);

// Polls GetUpdateStatus for permission holders only: the RPC is gated by
// instance:update, so non-holders never fire it. Transport failures stay
// silent because the global callout has no error surface; auth failures clear
// privileged data and flow through the shared auth handler.
export function useUpdateStatus(): UseUpdateStatusResult {
  const hasUpdatePermission = useHasPermission(INSTANCE_UPDATE_PERMISSION);
  const isAuthenticated = useIsAuthenticated();
  const permissions = usePermissions();
  const sessionExpiry = useSessionExpiry();
  const setPermissions = useSetPermissions();
  const { handleAuthErrors } = useAuthErrors();
  const [cachedStatus, setCachedStatus] = useState<CachedUpdateStatus | null>(null);
  const latestStatusRequest = useRef(0);
  const authBoundary = useMemo<AuthBoundarySnapshot>(
    () => ({ isAuthenticated, sessionExpiry, permissions }),
    [isAuthenticated, permissions, sessionExpiry],
  );

  const removeStaleUpdatePermission = useCallback(() => {
    const currentPermissions = useFleetStore.getState().auth.permissions;
    if (!currentPermissions.includes(INSTANCE_UPDATE_PERMISSION)) {
      return;
    }
    setPermissions(currentPermissions.filter((permission) => permission !== INSTANCE_UPDATE_PERMISSION));
  }, [setPermissions]);

  const fetchStatus = useCallback(
    async (invalidateCurrentStatus = false) => {
      const requestId = ++latestStatusRequest.current;
      const requestAuthBoundary = captureAuthBoundary();
      if (invalidateCurrentStatus) {
        // A release-channel mutation changes the meaning of the cached offer.
        // Hide it until this authoritative refresh returns.
        setCachedStatus(null);
      }

      try {
        const next = await instanceUpdateClient.getUpdateStatus({});
        if (
          requestId !== latestStatusRequest.current ||
          !isCurrentAuthBoundary(requestAuthBoundary) ||
          !currentlyHasUpdatePermission()
        ) {
          return;
        }
        // Most polls return identical data; keep the previous reference so
        // consumers don't re-render for a no-op tick.
        setCachedStatus((prev) =>
          prev && authBoundariesMatch(prev.authBoundary, requestAuthBoundary) && statusUnchanged(prev.response, next)
            ? prev
            : { response: next, authBoundary: requestAuthBoundary },
        );
      } catch (error) {
        if (requestId !== latestStatusRequest.current || !isCurrentAuthBoundary(requestAuthBoundary)) {
          return;
        }

        if (isAuthOrPermissionError(error)) {
          setCachedStatus(null);
        }
        if (isPermissionDeniedError(error)) {
          removeStaleUpdatePermission();
        }
        // Unauthenticated errors use the shared logout path. Other failures
        // stay silent and preserve the last known status unless a channel
        // mutation explicitly invalidated it above.
        handleAuthErrors({ error });
      }
    },
    [handleAuthErrors, removeStaleUpdatePermission],
  );

  const fetchData = useCallback(() => fetchStatus(), [fetchStatus]);

  useEffect(() => {
    if (!hasUpdatePermission || !isAuthenticated) {
      return undefined;
    }

    const refreshAfterInvalidation = () => {
      void fetchStatus(true);
    };
    window.addEventListener(UPDATE_STATUS_INVALIDATED_EVENT, refreshAfterInvalidation);
    return () => {
      window.removeEventListener(UPDATE_STATUS_INVALIDATED_EVENT, refreshAfterInvalidation);
    };
  }, [fetchStatus, hasUpdatePermission, isAuthenticated]);

  usePoll({
    fetchData,
    poll: true,
    pollIntervalMs: UPDATE_STATUS_POLL_INTERVAL_MS,
    enabled: hasUpdatePermission && isAuthenticated,
    params: authBoundary,
  });

  const status =
    hasUpdatePermission && isAuthenticated && cachedStatus?.authBoundary
      ? authBoundariesMatch(cachedStatus.authBoundary, authBoundary)
        ? cachedStatus.response
        : null
      : null;

  return { status, hasUpdatePermission };
}
