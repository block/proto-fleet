import { useCallback, useEffect, useRef, useState } from "react";
import { Navigate } from "react-router-dom";
import { instanceUpdateClient } from "@/protoFleet/api/clients";
import { ReleaseChannel } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAuthOrPermissionError, isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import { getSettingsLandingPath } from "@/protoFleet/config/navItems";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { copyInstallCommand } from "@/protoFleet/features/updates/copyInstallCommand";
import { useAuthErrors, useFleetStore, useHasPermission, usePermissions, useSetPermissions } from "@/protoFleet/store";
import { Copy } from "@/shared/assets/icons";
import Checkbox from "@/shared/components/Checkbox";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const SkeletonLoader = <SkeletonBar className="h-[22px] w-24" />;
const INSTANCE_UPDATE_PERMISSION = "instance:update";
const RELEASE_CHANNEL_SAVE_TIMEOUT_MS = 30_000;
const PERMISSION_REVOKED_MESSAGE = "You no longer have permission to update this instance";
const UPDATES_PAGE_DESCRIPTION = "View the server version and choose which releases this instance installs.";

interface AuthSessionSnapshot {
  isAuthenticated: boolean;
  sessionExpiry: Date | null;
}

const captureAuthSession = (): AuthSessionSnapshot => {
  const { isAuthenticated, sessionExpiry } = useFleetStore.getState().auth;
  return { isAuthenticated, sessionExpiry };
};

const isSameAuthSession = (snapshot: AuthSessionSnapshot) => {
  const { isAuthenticated, sessionExpiry } = useFleetStore.getState().auth;
  // Login installs a new Date object and logout clears it. Compare identity so
  // even a replacement session for the same user cannot inherit old failures.
  return isAuthenticated === snapshot.isAuthenticated && sessionExpiry === snapshot.sessionExpiry;
};

// A route remount must not load status while the previous page instance is
// still saving a channel. Otherwise it can briefly expose the old channel's
// install command after navigation away and back.
let inFlightReleaseChannelSave: Promise<unknown> | null = null;

const saveReleaseChannel = (channel: ReleaseChannel) => {
  // This promise is shared across route instances, so it must have a bounded
  // lifetime. A timed-out write is reconciled by the authoritative status
  // fetch that follows on the current or next page instance.
  const save = instanceUpdateClient.setReleaseChannel({ channel }, { timeoutMs: RELEASE_CHANNEL_SAVE_TIMEOUT_MS });
  inFlightReleaseChannelSave = save;
  void save.then(
    () => {
      if (inFlightReleaseChannelSave === save) {
        inFlightReleaseChannelSave = null;
      }
    },
    () => {
      if (inFlightReleaseChannelSave === save) {
        inFlightReleaseChannelSave = null;
      }
    },
  );
  return save;
};

const waitForReleaseChannelSave = async () => {
  const save = inFlightReleaseChannelSave;
  if (!save) {
    return;
  }
  try {
    await save;
  } catch {
    // A failed save can still be ambiguous (for example, a lost response
    // after the server committed), so the caller must fetch authoritative
    // status after the mutation settles either way.
  }
};

const Updates = () => {
  const canUpdateInstance = useHasPermission(INSTANCE_UPDATE_PERMISSION);
  const permissions = usePermissions();
  const setPermissions = useSetPermissions();
  const { handleAuthErrors } = useAuthErrors();
  const [status, setStatus] = useState<GetUpdateStatusResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [isChannelChangePending, setIsChannelChangePending] = useState(false);
  const latestStatusRequest = useRef(0);
  const isMounted = useRef(false);
  // The channel the server has persisted; the checkbox is controlled by it,
  // so a failed save never moves the control.
  const [channel, setChannel] = useState<ReleaseChannel>(ReleaseChannel.UNSPECIFIED);

  const handlePermissionRevoked = useCallback(
    (notify: boolean) => {
      if (notify) {
        pushToast({
          message: PERMISSION_REVOKED_MESSAGE,
          status: STATUSES.error,
        });
      }
      // A delayed response can arrive after this component unmounts. Read the
      // current store value so it cannot overwrite newer permission changes.
      const currentPermissions = useFleetStore.getState().auth.permissions;
      setPermissions(currentPermissions.filter((permission) => permission !== INSTANCE_UPDATE_PERMISSION));
    },
    [setPermissions],
  );

  const fetchStatus = useCallback(async () => {
    const requestId = ++latestStatusRequest.current;
    const authSession = captureAuthSession();
    await waitForReleaseChannelSave();
    if (requestId !== latestStatusRequest.current || !isSameAuthSession(authSession)) {
      return;
    }
    try {
      const response = await instanceUpdateClient.getUpdateStatus({});
      if (requestId !== latestStatusRequest.current || !isSameAuthSession(authSession)) {
        return;
      }
      setStatus(response);
      setChannel(response.channel);
      setLoadError(null);
    } catch (err) {
      if (!isSameAuthSession(authSession)) {
        return;
      }
      const shouldUpdatePage = requestId === latestStatusRequest.current && isMounted.current;
      handleAuthErrors({
        error: err,
        onError: () => {
          if (isPermissionDeniedError(err)) {
            handlePermissionRevoked(shouldUpdatePage);
            return;
          }
          if (!shouldUpdatePage || isAuthOrPermissionError(err)) {
            return;
          }
          setLoadError(getErrorMessage(err, "Failed to load update status"));
        },
      });
    }
  }, [handleAuthErrors, handlePermissionRevoked]);

  useEffect(() => {
    isMounted.current = true;
    return () => {
      isMounted.current = false;
    };
  }, []);

  useEffect(() => {
    // The RPC is server-gated on instance:update; non-holders are redirected
    // below and must not fire it.
    if (!canUpdateInstance) {
      return;
    }
    void fetchStatus();
    return () => {
      // Ignore a response that arrives after permission loss, unmount, or a
      // dependency-driven replacement request.
      latestStatusRequest.current += 1;
    };
  }, [canUpdateInstance, fetchStatus]);

  const handleIncludeRCChange = async (includeRC: boolean) => {
    const nextChannel = includeRC ? ReleaseChannel.STABLE_AND_RC : ReleaseChannel.STABLE;
    if (nextChannel === channel || isChannelChangePending) {
      return;
    }
    const authSession = captureAuthSession();
    setIsChannelChangePending(true);
    try {
      let saveSucceeded = false;
      try {
        await saveReleaseChannel(nextChannel);
        saveSucceeded = true;
      } catch (err) {
        if (!isSameAuthSession(authSession)) {
          return;
        }
        const shouldUpdatePage = isMounted.current;
        handleAuthErrors({
          error: err,
          onError: () => {
            if (isPermissionDeniedError(err)) {
              handlePermissionRevoked(shouldUpdatePage);
              return;
            }
            if (!shouldUpdatePage || isAuthOrPermissionError(err)) {
              return;
            }
            pushToast({
              message: getErrorMessage(err, "Failed to update release channel"),
              status: STATUSES.error,
            });
          },
        });
        if (!shouldUpdatePage || isAuthOrPermissionError(err)) {
          return;
        }
      }

      if (!isMounted.current || !isSameAuthSession(authSession)) {
        return;
      }
      if (saveSucceeded) {
        setChannel(nextChannel);
        pushToast({
          message: "Release channel updated",
          status: STATUSES.success,
        });
      }
      // The eligible release differs per channel. Refresh after both success
      // and an ambiguous non-auth save failure so the server remains the
      // authority for the checkbox, offered version, and install command.
      await fetchStatus();
    } finally {
      if (isMounted.current && isSameAuthSession(authSession)) {
        setIsChannelChangePending(false);
      }
    }
  };

  // Redirect callers without instance:update away — placed after all
  // hooks to satisfy rules-of-hooks.
  if (!canUpdateInstance) {
    return <Navigate to={getSettingsLandingPath(permissions)} replace />;
  }

  const release = status?.statusAvailable && status.updateAvailable ? status.latestEligible : undefined;

  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Updates" description={UPDATES_PAGE_DESCRIPTION} />
      {loadError ? (
        <SettingsEmptyState size="section" title="Unable to load update status" description={loadError} />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Server version" titleSize="text-heading-200" />
            <div>
              <Row className="flex justify-between" divider>
                <div className="text-300">Current version</div>
                <div className="text-300">{status?.currentVersion ?? SkeletonLoader}</div>
              </Row>
              {release ? (
                <>
                  <Row className="flex justify-between" divider>
                    <div className="text-300">Latest available</div>
                    <div className="flex items-center gap-3">
                      <span className="text-300">{release.version}</span>
                      {/* The server blanks non-https notes URLs, so an empty
                          string means "no link to offer". */}
                      {release.releaseNotesUrl ? (
                        <a
                          href={release.releaseNotesUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-300 text-text-primary-70 underline underline-offset-2 hover:text-text-primary"
                        >
                          Release notes
                        </a>
                      ) : null}
                    </div>
                  </Row>
                  <Row className="flex items-center justify-between gap-2" divider={false}>
                    <code className="min-w-0 truncate font-mono text-200 text-text-primary-70">
                      {status?.installCommand}
                    </code>
                    <button
                      type="button"
                      disabled={isChannelChangePending}
                      onClick={() => copyInstallCommand(status?.installCommand ?? "")}
                      className="flex h-8 shrink-0 items-center gap-2 rounded-lg bg-core-primary-10 px-2 text-200 whitespace-nowrap text-text-primary hover:cursor-pointer hover:bg-core-primary-20 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <Copy width="w-4" />
                      Copy install command
                    </button>
                  </Row>
                </>
              ) : (
                <Row className="flex justify-between" divider={false}>
                  <div className="text-300">
                    {status
                      ? status.statusAvailable
                        ? "You're on the latest version"
                        : "Update status unavailable"
                      : SkeletonLoader}
                  </div>
                </Row>
              )}
            </div>
          </div>
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Release channel" titleSize="text-heading-200" />
            {status ? (
              <label className="flex w-fit cursor-pointer items-center gap-3">
                <Checkbox
                  className="shrink-0"
                  checked={channel === ReleaseChannel.STABLE_AND_RC}
                  disabled={isChannelChangePending}
                  onChange={(e) => void handleIncludeRCChange(e.target.checked)}
                />
                <span className="text-300">Include release candidates</span>
              </label>
            ) : (
              <SkeletonBar className="h-8 w-44" />
            )}
            <p className="text-200 text-text-primary-50">
              An RC install cannot downgrade until the next stable release.
            </p>
          </div>
        </div>
      )}
    </div>
  );
};

export default Updates;
