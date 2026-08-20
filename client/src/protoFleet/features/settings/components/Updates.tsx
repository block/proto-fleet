import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { Navigate } from "react-router-dom";
import { instanceUpdateClient } from "@/protoFleet/api/clients";
import { ReleaseChannel, UpgradePhase } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/instance/v1/updates_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAuthOrPermissionError, isPermissionDeniedError } from "@/protoFleet/api/requestErrors";
import { getSettingsLandingPath } from "@/protoFleet/config/navItems";
import AvailableUpdateAnimation from "@/protoFleet/features/settings/components/AvailableUpdateAnimation";
import ManualInstallModal from "@/protoFleet/features/settings/components/ManualInstallModal";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import UpgradeOperationModal from "@/protoFleet/features/settings/components/UpgradeOperationModal";
import { getUpgradeProgressCopy } from "@/protoFleet/features/settings/components/upgradeProgressCopy";
import {
  getUpgradeOperationOutcomeKey,
  isUpgradeActive,
  useUpgradeOperation,
} from "@/protoFleet/features/updates/api/useUpgradeOperation";
import {
  useAuthErrors,
  useFleetStore,
  useHasPermission,
  usePermissions,
  useSessionGeneration,
  useSetPermissions,
  useUsername,
} from "@/protoFleet/store";
import { Info, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import Switch from "@/shared/components/Switch";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const SkeletonLoader = <SkeletonBar className="h-[22px] w-24" />;
const INSTANCE_UPDATE_PERMISSION = "instance:update";
const UPDATE_STATUS_REQUEST_TIMEOUT_MS = 10_000;
const RELEASE_CHANNEL_SAVE_TIMEOUT_MS = 30_000;
const PERMISSION_REVOKED_MESSAGE = "You no longer have permission to update this instance";
const UPDATES_PAGE_DESCRIPTION =
  "View your Fleet version, install updates, and choose whether to include release candidates.";

type OpenDialog = "manual-install" | "upgrade" | null;

interface UpdateStatusLockupProps {
  action?: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  testId?: string;
  title: ReactNode;
}

const UpdateStatusLockup = ({ action, description, icon, testId, title }: UpdateStatusLockupProps) => (
  <div data-testid={testId} className="flex items-center gap-4 rounded-xl bg-core-primary-5 p-6">
    {icon ? <span className="shrink-0">{icon}</span> : null}
    <div className="min-w-0 grow">
      <div className="text-emphasis-300 text-text-primary">{title}</div>
      {description ? <p className="text-300 text-text-primary-70">{description}</p> : null}
    </div>
    {action ? <div className="shrink-0">{action}</div> : null}
  </div>
);

interface UpdateFeatureCardProps {
  action?: ReactNode;
  children: ReactNode;
  testId: string;
}

const UpdateFeatureCard = ({ action, children, testId }: UpdateFeatureCardProps) => (
  <div
    data-testid={testId}
    className="flex min-h-[240px] flex-col overflow-hidden rounded-xl bg-core-primary-5 tablet:flex-row"
  >
    <AvailableUpdateAnimation />
    <div className="flex grow flex-col items-center gap-5 px-6 pt-0 pb-8 text-center tablet:flex-row tablet:justify-between tablet:py-8 tablet:pr-6 tablet:pl-0 tablet:text-left">
      <div className="space-y-1">{children}</div>
      {action}
    </div>
  </div>
);

interface AuthSessionSnapshot {
  identity: string;
  isAuthenticated: boolean;
  sessionExpiry: Date | null;
}

interface AuthBoundStatus {
  authSessionIdentity: string;
  response: GetUpdateStatusResponse;
}

interface AuthBoundError {
  authSessionIdentity: string;
  message: string;
}

const captureAuthSession = (identity: string): AuthSessionSnapshot => {
  const { isAuthenticated, sessionExpiry } = useFleetStore.getState().auth;
  return { identity, isAuthenticated, sessionExpiry };
};

const isSameAuthSession = (snapshot: AuthSessionSnapshot) => {
  const { isAuthenticated, sessionExpiry, sessionGeneration, username } = useFleetStore.getState().auth;
  return (
    `${username}:${sessionGeneration}` === snapshot.identity &&
    isAuthenticated === snapshot.isAuthenticated &&
    sessionExpiry === snapshot.sessionExpiry
  );
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
  const sessionGeneration = useSessionGeneration();
  const setPermissions = useSetPermissions();
  const username = useUsername();
  const authSessionIdentity = `${username}:${sessionGeneration}`;
  const { handleAuthErrors } = useAuthErrors();
  const [statusState, setStatusState] = useState<AuthBoundStatus | null>(null);
  const [loadErrorState, setLoadErrorState] = useState<AuthBoundError | null>(null);
  const [isChannelChangePending, setIsChannelChangePending] = useState(false);
  const [isStatusRefreshPending, setIsStatusRefreshPending] = useState(false);
  const [openDialog, setOpenDialog] = useState<OpenDialog>(null);
  const [upgradeReloadState, setUpgradeReloadState] = useState({
    authSessionIdentity,
    pending: false,
  });
  const isUpgradeReloadPending =
    upgradeReloadState.authSessionIdentity === authSessionIdentity && upgradeReloadState.pending;
  const latestStatusRequest = useRef(0);
  const isMounted = useRef(false);
  const lastAutoOpenedOperation = useRef<string | null>(null);
  const previousManualFallbackReady = useRef(false);
  const previousReconciling = useRef(false);
  const previousTriggerError = useRef<string | null>(null);
  const hadSurfacedOperation = useRef(false);
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

  // Shared policy for request failures: revoked permissions demote the page,
  // auth errors are swallowed (the auth layer redirects), and anything else is
  // surfaced through the caller's sink. shouldUpdatePage stays caller-derived
  // because staleness semantics differ per request.
  const handleRequestError = useCallback(
    (err: unknown, shouldUpdatePage: boolean, surfaceError: () => void) => {
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
          surfaceError();
        },
      });
    },
    [handleAuthErrors, handlePermissionRevoked],
  );

  const handleUpgradePollError = useCallback(
    (error: unknown) => {
      handleAuthErrors({
        error,
        onError: () => {
          if (isPermissionDeniedError(error)) {
            handlePermissionRevoked(isMounted.current);
          }
        },
      });
    },
    [handleAuthErrors, handlePermissionRevoked],
  );

  const fetchStatus = useCallback(async () => {
    const requestId = ++latestStatusRequest.current;
    const authSession = captureAuthSession(authSessionIdentity);
    setIsStatusRefreshPending(true);
    try {
      await waitForReleaseChannelSave();
      if (requestId !== latestStatusRequest.current || !isSameAuthSession(authSession)) {
        return;
      }
      const response = await instanceUpdateClient.getUpdateStatus({}, { timeoutMs: UPDATE_STATUS_REQUEST_TIMEOUT_MS });
      if (requestId !== latestStatusRequest.current || !isSameAuthSession(authSession)) {
        return;
      }
      setStatusState({ authSessionIdentity, response });
      setChannel(response.channel);
      setLoadErrorState(null);
    } catch (err) {
      if (!isSameAuthSession(authSession)) {
        return;
      }
      const shouldUpdatePage = requestId === latestStatusRequest.current && isMounted.current;
      handleRequestError(err, shouldUpdatePage, () => {
        setOpenDialog(null);
        setLoadErrorState({
          authSessionIdentity,
          message: getErrorMessage(err, "We couldn't load update status"),
        });
      });
    } finally {
      if (requestId === latestStatusRequest.current && isSameAuthSession(authSession) && isMounted.current) {
        setIsStatusRefreshPending(false);
      }
    }
  }, [authSessionIdentity, handleRequestError]);

  const upgrade = useUpgradeOperation({
    authSessionIdentity,
    enabled: canUpdateInstance,
    onUntrackedSuccess: () => {
      void fetchStatus();
    },
    onPollError: handleUpgradePollError,
  });
  const activeUpgrade = isUpgradeActive(upgrade.operation);
  const succeededUpgrade = upgrade.operation?.phase === UpgradePhase.SUCCEEDED;
  const upgradeRequestPending = upgrade.triggering || upgrade.reconciling;
  const unresolvedTrackedUpgrade = Boolean(upgrade.trackedTargetVersion && !upgrade.operation);
  const upgradeLocksConfiguration =
    isStatusRefreshPending ||
    upgrade.operationStatusPending ||
    upgradeRequestPending ||
    unresolvedTrackedUpgrade ||
    Boolean(upgrade.operation);
  const upgradeActionDisabled =
    isChannelChangePending ||
    isStatusRefreshPending ||
    upgrade.operationStatusPending ||
    upgradeRequestPending ||
    unresolvedTrackedUpgrade;
  const hasCurrentLoadError = loadErrorState?.authSessionIdentity === authSessionIdentity;
  const hasCurrentStatus = statusState?.authSessionIdentity === authSessionIdentity;
  const manualCommandDisabled =
    isChannelChangePending ||
    isStatusRefreshPending ||
    hasCurrentLoadError ||
    !hasCurrentStatus ||
    upgrade.operationStatusPending ||
    activeUpgrade ||
    upgradeRequestPending ||
    unresolvedTrackedUpgrade ||
    Boolean(succeededUpgrade);

  useEffect(() => {
    isMounted.current = true;
    return () => {
      isMounted.current = false;
    };
  }, []);

  useEffect(() => {
    setOpenDialog(null);
  }, [authSessionIdentity]);

  useEffect(() => {
    const operation = upgrade.operation;
    const terminal = operation?.phase === UpgradePhase.SUCCEEDED || operation?.phase === UpgradePhase.FAILED;
    if (!operation || upgrade.operationStatusPending || !terminal) {
      return;
    }
    const operationIdentity = getUpgradeOperationOutcomeKey(operation);
    const autoOpenKey = operationIdentity ? `${authSessionIdentity}:terminal:${operationIdentity}` : undefined;
    if (autoOpenKey && lastAutoOpenedOperation.current === autoOpenKey) {
      return;
    }
    // Open once when an operation is first recovered, and once more when it
    // becomes terminal. Intermediate phase updates must not keep stealing
    // focus after an operator dismisses the progress modal.
    // Missing or invalid host identity fails open so it cannot suppress a
    // distinct operation or rewritten terminal outcome.
    lastAutoOpenedOperation.current = autoOpenKey ?? null;
    setOpenDialog("upgrade");
  }, [authSessionIdentity, upgrade.operation, upgrade.operationStatusPending]);

  useEffect(() => {
    // Once the host exposes an operation, its detail dialog is authoritative.
    // Do not let a previously opened generic manual-install dialog cover it or
    // expose a stale command while an outcome is being handled.
    if (upgrade.operation) {
      setOpenDialog((current) => (current === "manual-install" ? null : current));
    }
  }, [upgrade.operation]);

  useEffect(() => {
    const hadOperation = hadSurfacedOperation.current;
    hadSurfacedOperation.current = Boolean(upgrade.operation);
    // Polling can remove the surfaced operation when another session durably
    // dismissed it. Leaving the modal open would morph the outcome dialog
    // into a confirmation for starting the same upgrade again. Local dismiss
    // paths close the modal themselves before clearing, so this is a no-op
    // for them; a pending reconciliation or trigger error keeps its dialog.
    if (hadOperation && !upgrade.operation && !upgrade.reconciling && !upgrade.triggerError) {
      setOpenDialog(null);
    }
  }, [upgrade.operation, upgrade.reconciling, upgrade.triggerError]);

  useEffect(() => {
    const wasReconciling = previousReconciling.current;
    if (wasReconciling && !upgrade.reconciling && !upgrade.operation && upgrade.triggerError) {
      // A reachable executor authoritatively found no matching operation.
      // Refresh the offer before allowing a retry because the release/channel
      // may have changed while the trigger outcome was being reconciled.
      void fetchStatus();
    }
    previousReconciling.current = upgrade.reconciling;
  }, [fetchStatus, upgrade.operation, upgrade.reconciling, upgrade.triggerError]);

  useEffect(() => {
    if (upgrade.manualFallbackReady && !previousManualFallbackReady.current) {
      setOpenDialog("upgrade");
    }
    previousManualFallbackReady.current = upgrade.manualFallbackReady;
  }, [upgrade.manualFallbackReady]);

  useEffect(() => {
    if (upgrade.triggerError && upgrade.triggerError !== previousTriggerError.current && !upgrade.reconciling) {
      setOpenDialog("upgrade");
      void fetchStatus();
    }
    previousTriggerError.current = upgrade.triggerError;
  }, [fetchStatus, upgrade.reconciling, upgrade.triggerError]);

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
    if (nextChannel === channel || isChannelChangePending || upgradeLocksConfiguration) {
      return;
    }
    const authSession = captureAuthSession(authSessionIdentity);
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
        handleRequestError(err, shouldUpdatePage, () => {
          pushToast({
            message: getErrorMessage(err, "We couldn't update the release candidate setting"),
            status: STATUSES.error,
          });
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
          message: includeRC ? "Release candidates turned on" : "Release candidates turned off",
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

  const handleSuccessfulUpgradeReload = async () => {
    if (isUpgradeReloadPending) {
      return;
    }
    const authSession = captureAuthSession(authSessionIdentity);
    setUpgradeReloadState({ authSessionIdentity, pending: true });
    try {
      // The success record is durable host state. Clear the exact outcome
      // before reloading so the next page does not surface and lock on it
      // again. A failed acknowledgement deliberately leaves it visible.
      await upgrade.acknowledgeOperation();
      if (!isMounted.current || !isSameAuthSession(authSession)) {
        return;
      }
      upgrade.reloadFleet();
    } catch (err) {
      if (!isSameAuthSession(authSession)) {
        return;
      }
      handleRequestError(err, isMounted.current, () => {
        pushToast({
          message: `Fleet wasn't reloaded: ${getErrorMessage(err, "the completed upgrade couldn't be recorded")}`,
          status: STATUSES.error,
        });
      });
    } finally {
      if (isMounted.current && isSameAuthSession(authSession)) {
        setUpgradeReloadState({ authSessionIdentity, pending: false });
      }
    }
  };

  // Redirect callers without instance:update away — placed after all
  // hooks to satisfy rules-of-hooks.
  if (!canUpdateInstance) {
    return <Navigate to={getSettingsLandingPath(permissions)} replace />;
  }

  const currentStatus = hasCurrentStatus ? statusState.response : null;
  const currentLoadError = hasCurrentLoadError ? loadErrorState.message : null;
  const release =
    currentStatus?.statusAvailable && currentStatus.updateAvailable ? currentStatus.latestEligible : undefined;
  const modalRelease =
    isStatusRefreshPending ||
    currentLoadError ||
    (upgrade.operation && upgrade.operation.targetVersion !== release?.version)
      ? undefined
      : release;
  const operationStatusLabel = upgrade.reconciling
    ? "Checking update status"
    : upgrade.operation?.phase === UpgradePhase.FAILED
      ? upgrade.operation.recoveryCommand.trim()
        ? "Update needs recovery"
        : "Update couldn't complete"
      : upgrade.operation?.phase === UpgradePhase.SUCCEEDED
        ? "Update complete"
        : upgrade.operation
          ? getUpgradeProgressCopy(upgrade.operation.phase)
          : upgrade.triggerError
            ? "We couldn't start the update"
            : null;
  const operationStatusDescription = upgrade.reconciling
    ? upgrade.manualFallbackReady
      ? "Before installing manually, check whether the host is already updating."
      : "We're checking whether the update started."
    : upgrade.operation?.phase === UpgradePhase.FAILED
      ? upgrade.operation.recoveryCommand.trim()
        ? undefined
        : "Review the host log before you update again."
      : upgrade.operation?.phase === UpgradePhase.SUCCEEDED
        ? "Relaunch Fleet to use the new version."
        : null;
  const hasUpgradeDetails = Boolean(upgrade.operation || upgrade.reconciling || upgrade.triggerError);
  const showCurrentVersionCard = Boolean(currentStatus?.statusAvailable && !release && !hasUpgradeDetails);
  const showUpdateStatusUnavailable = Boolean(currentStatus && !currentStatus.statusAvailable && !hasUpgradeDetails);
  const canViewUpgradeDetails = Boolean(
    upgrade.operation || upgrade.manualFallbackReady || (upgrade.triggerError && !upgrade.reconciling),
  );
  const upgradeDetailsButtonText =
    upgrade.operation?.phase === UpgradePhase.FAILED && upgrade.operation.recoveryCommand.trim()
      ? "View recovery steps"
      : "View update details";

  return (
    <div className="flex flex-col gap-6">
      <UpgradeOperationModal
        connectionLost={upgrade.connectionLost}
        manualFallbackReady={upgrade.manualFallbackReady}
        onAcknowledge={() => {
          const authSession = captureAuthSession(authSessionIdentity);
          void upgrade
            .acknowledgeOperation()
            .then(() => {
              if (isMounted.current && isSameAuthSession(authSession)) {
                setOpenDialog(null);
              }
            })
            .catch((err: unknown) => {
              if (!isSameAuthSession(authSession)) {
                return;
              }
              handleRequestError(err, isMounted.current, () => {
                pushToast({
                  message: getErrorMessage(err, "Fleet could not mark this update as resolved"),
                  status: STATUSES.error,
                });
              });
            });
        }}
        onDismiss={() => setOpenDialog(null)}
        onReload={() => void handleSuccessfulUpgradeReload()}
        onUpgrade={upgrade.triggerUpgrade}
        onUseManualFallback={() => {
          upgrade.useManualFallback();
          setOpenDialog(null);
          void fetchStatus();
        }}
        open={openDialog === "upgrade"}
        operation={upgrade.operation}
        reconciling={upgrade.reconciling}
        reloadPending={isUpgradeReloadPending}
        release={modalRelease}
        targetVersion={
          upgrade.operation?.targetVersion ?? (upgrade.reconciling ? upgrade.trackedTargetVersion : release?.version)
        }
        triggerError={upgrade.triggerError}
        triggering={upgrade.triggering}
      />
      {currentStatus?.installCommand && !currentLoadError ? (
        <ManualInstallModal
          copyDisabled={manualCommandDisabled}
          open={openDialog === "manual-install"}
          onDismiss={() => setOpenDialog(null)}
          installCommand={currentStatus.installCommand}
          version={release?.version ?? currentStatus.currentVersion}
        />
      ) : null}
      <SettingsPageHeader title="Software Update" description={UPDATES_PAGE_DESCRIPTION} />
      {currentLoadError && hasUpgradeDetails ? (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-border-5 p-6">
          <div className="min-w-0">
            <div className="text-heading-100 text-text-primary">Update status</div>
            <div className="truncate text-200 text-text-primary-50">{operationStatusLabel}</div>
          </div>
          {canViewUpgradeDetails ? (
            <Button
              variant={variants.secondary}
              text={upgradeDetailsButtonText}
              onClick={() => setOpenDialog("upgrade")}
            />
          ) : null}
        </div>
      ) : null}
      {currentLoadError ? (
        <SettingsEmptyState size="section" title="We couldn't load update status" description={currentLoadError} />
      ) : (
        <div className="flex flex-col gap-4">
          {release && !hasUpgradeDetails ? (
            <UpdateFeatureCard
              testId="available-update-lockup"
              action={
                currentStatus?.oneClickAvailable || currentStatus?.installCommand ? (
                  <div className="flex shrink-0 flex-wrap items-center justify-center gap-2 tablet:justify-end">
                    {currentStatus?.oneClickAvailable ? (
                      <Button
                        variant={variants.primary}
                        text="Update now"
                        disabled={upgradeActionDisabled}
                        onClick={() => setOpenDialog("upgrade")}
                      />
                    ) : null}
                    {currentStatus?.installCommand ? (
                      <Button
                        variant={currentStatus.oneClickAvailable ? variants.secondary : variants.primary}
                        text="Install manually"
                        disabled={manualCommandDisabled}
                        onClick={() => setOpenDialog("manual-install")}
                      />
                    ) : null}
                  </div>
                ) : undefined
              }
            >
              <div className="text-heading-300 text-text-primary">Fleet {release.version} available</div>
              {!currentStatus?.oneClickAvailable ? (
                <p className="text-300 text-text-primary-70">
                  In-app updates aren't available for this release. Install it manually instead.
                </p>
              ) : null}
              {release.releaseNotesUrl ? (
                <a
                  href={release.releaseNotesUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-block text-300 text-text-primary-70 underline underline-offset-2 hover:text-text-primary"
                >
                  Release notes
                </a>
              ) : null}
            </UpdateFeatureCard>
          ) : null}
          {activeUpgrade ? (
            <UpdateFeatureCard
              testId="active-update-lockup"
              action={
                <Button
                  variant={variants.secondary}
                  text="Updating"
                  prefixIcon={<ProgressCircular dataTestId="update-status-spinner" size={16} indeterminate />}
                  onClick={() => setOpenDialog("upgrade")}
                />
              }
            >
              <div className="text-heading-300 text-text-primary">
                Updating Fleet to {upgrade.operation?.targetVersion}
              </div>
              <p className="text-300 text-text-primary-70">{operationStatusLabel}</p>
            </UpdateFeatureCard>
          ) : null}
          {showCurrentVersionCard ? (
            <UpdateStatusLockup
              testId="current-version-lockup"
              icon={
                <span data-testid="current-version-success-icon">
                  <Success className="text-intent-success-fill" />
                </span>
              }
              title="Fleet is up to date"
              description={`Current version: ${currentStatus?.currentVersion}`}
            />
          ) : null}
          {hasUpgradeDetails && !activeUpgrade ? (
            <UpdateStatusLockup
              testId="update-status-lockup"
              title={operationStatusLabel || "Update status"}
              description={operationStatusDescription}
              action={
                canViewUpgradeDetails ? (
                  <Button
                    variant={variants.secondary}
                    size={sizes.compact}
                    text={upgradeDetailsButtonText}
                    onClick={() => setOpenDialog("upgrade")}
                  />
                ) : undefined
              }
            />
          ) : null}
          {showUpdateStatusUnavailable ? (
            <UpdateStatusLockup
              testId="update-status-unavailable-lockup"
              icon={<Info className="text-text-primary-70" />}
              title="Update status unavailable"
              description={`Current version: ${currentStatus?.currentVersion}`}
            />
          ) : null}
          {!showCurrentVersionCard && !showUpdateStatusUnavailable ? (
            <div className="rounded-xl border border-border-5 px-6">
              <Row testId="current-version-row" className="flex items-center justify-between gap-3" divider={false}>
                <span className="text-300">Current version</span>
                <span className="text-300">{currentStatus?.currentVersion ?? SkeletonLoader}</span>
              </Row>
            </div>
          ) : null}
          <div className="rounded-xl border border-border-5 px-6">
            {currentStatus ? (
              <Row className="flex items-center gap-3" divider={false}>
                <Switch
                  ariaLabel="Include release candidates"
                  checked={channel === ReleaseChannel.STABLE_AND_RC}
                  disabled={isChannelChangePending || upgradeLocksConfiguration}
                  id="include-release-candidates"
                  setChecked={(next) => {
                    const includeRC =
                      typeof next === "function" ? next(channel === ReleaseChannel.STABLE_AND_RC) : next;
                    void handleIncludeRCChange(includeRC);
                  }}
                />
                <label htmlFor="include-release-candidates" className="min-w-0 cursor-pointer">
                  <span>
                    <span className="block text-300">Include release candidates</span>
                    <span className="block text-200 text-text-primary-50">
                      After you install a release candidate, you can't return to an earlier release until the next
                      stable release.
                    </span>
                  </span>
                </label>
              </Row>
            ) : (
              <div className="py-3">
                <SkeletonBar className="h-8 w-44" />
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default Updates;
