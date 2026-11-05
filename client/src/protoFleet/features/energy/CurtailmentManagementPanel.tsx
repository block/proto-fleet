import { type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";
import { Code, ConnectError } from "@connectrpc/connect";
import { createCurtailmentResponseProfileOption, createSiteOptions } from "./curtailmentProfileModel";
import { CurtailmentForceReleaseDialog, CurtailmentRecoveryTerminateDialog } from "./CurtailmentRecoveryDialogs";

import {
  type ActiveCurtailmentEvent,
  type CurtailmentEventState,
  type CurtailmentHistoryEvent,
  type CurtailmentPlanPreview,
  type CurtailmentSiteOption,
  type CurtailmentStartModalMode,
  type CurtailmentSubmitValues,
  isEditableResponseProfile,
} from "@/protoFleet/api/curtailmentTypes";

import { getErrorCause } from "@/protoFleet/api/requestErrors";
import { buildSiteNameById } from "@/protoFleet/api/siteNames";
import { useSites } from "@/protoFleet/api/sites";
import {
  adminTerminateReasonRequiredMessage,
  type ForceReleaseCurtailmentOptions,
  type AdminTerminateCurtailmentOptions as TerminateRecoveryOptions,
  useCurtailmentApi,
} from "@/protoFleet/api/useCurtailmentApi";
import { useCurtailmentPolling } from "@/protoFleet/api/useCurtailmentPolling";
import useCurtailmentResponseProfiles from "@/protoFleet/api/useCurtailmentResponseProfiles";
import useInfrastructureDevices from "@/protoFleet/api/useInfrastructureDevices";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import ActiveCurtailmentStatus from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import { getDefaultCurtailmentSiteScope } from "@/protoFleet/features/energy/curtailmentSiteScopeDefaults";
import CurtailmentStartModal from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { createCurtailmentPlanPreview } from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface CurtailmentManagementPanelProps {
  enableManage?: boolean;
  enableRecover?: boolean;
  className?: string;
}

interface PendingStopConfirmation {
  action: CurtailmentStopConfirmationAction;
  eventId: string;
}

interface EditCurtailmentSession {
  eventId: string;
  initialValues: CurtailmentSubmitValues;
  preview: CurtailmentPlanPreview;
}

interface CurtailmentMessageProps {
  message: string;
}

const activeCurtailmentRefreshIntervalMs = 3_000;
const nonTerminalActiveEventStates = new Set<CurtailmentEventState>(["pending", "active", "restoring"]);
const updateableCurtailmentEventStates = new Set<CurtailmentEventState>(["pending", "active"]);
const forceRestorableCurtailmentEventStates = new Set<CurtailmentEventState>(["pending", "active"]);

const automationRestoreBlockedErrorPrefix = "cannot restore automation-owned curtailment event";
const transientTerminateRecoveryErrorCodes = new Set<Code>([
  Code.Aborted,
  Code.Canceled,
  Code.DeadlineExceeded,
  Code.Unavailable,
  // connect-web assigns Code.Unknown to raw transport failures it cannot
  // classify (ConnectError.from's default code), so a network blip must not
  // unlock the destructive Abort escalation. Server-side rejections arrive
  // with definitive codes via the fleeterror mapping interceptor.
  Code.Unknown,
]);

function getConnectError(error: unknown): ConnectError | null {
  if (error instanceof ConnectError) {
    return error;
  }

  const cause = getErrorCause(error);
  return cause instanceof ConnectError ? cause : null;
}

function shouldOfferForceReleaseAfterTerminateRecoveryError(error: unknown): boolean {
  if (error instanceof Error && error.message === adminTerminateReasonRequiredMessage) {
    return false;
  }

  const connectError = getConnectError(error);
  return !connectError || !transientTerminateRecoveryErrorCodes.has(connectError.code);
}

function getRecoveryStopErrorMessage(
  stopError: string | null,
  isAutomationOwned: boolean | undefined,
  canUseRecovery: boolean,
): string | null {
  if (!stopError) {
    return null;
  }
  if (!isAutomationOwned) {
    return stopError;
  }
  if (stopError.startsWith(automationRestoreBlockedErrorPrefix)) {
    return canUseRecovery
      ? "Automation is still requesting curtailment. Use Abort to cancel this event and disable the automation before restoring miners."
      : "Automation is still requesting curtailment. Ask an admin to Abort this event and disable the automation before restoring miners.";
  }
  return stopError;
}

function CurtailmentMessage({ message }: CurtailmentMessageProps): ReactElement {
  return (
    <div className="flex items-center gap-3 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
      <Alert className="shrink-0 text-intent-warning-fill" />
      <span className="text-emphasis-300">{message}</span>
    </div>
  );
}

function createActiveCurtailmentPreview(
  event: ActiveCurtailmentEvent,
  values: CurtailmentSubmitValues,
): CurtailmentPlanPreview {
  return createCurtailmentPlanPreview(values, {
    selectedMinerCount: event.selectedMiners,
    targetKw: event.targetKw,
    estimatedReductionKw: event.estimatedReductionKw,
  });
}

function canUpdateCurtailmentEvent(event: ActiveCurtailmentEvent): boolean {
  return updateableCurtailmentEventStates.has(event.state);
}

function canTerminateRecoveryCurtailmentEvent(event: ActiveCurtailmentEvent): boolean {
  return Boolean(event.isAutomationOwned && event.state === "restoring");
}

function canAbortCurtailmentOwnership(event: ActiveCurtailmentEvent): boolean {
  return (
    event.state === "restoring" || (event.isAutomationOwned && forceRestorableCurtailmentEventStates.has(event.state))
  );
}

function CurtailmentManagementPanel({
  enableManage = true,
  enableRecover = false,
  className,
}: CurtailmentManagementPanelProps): ReactElement {
  const navigate = useNavigate();
  const canReadSiteCatalog = useHasPermission("site:read");
  const canReadRackCatalog = useHasPermission("rack:read");
  const { activeSite } = useActiveSite({});
  const { listSites } = useSites();
  const [loadedSiteNameById, setLoadedSiteNameById] = useState(() => new Map<string, string>());
  const [siteOptions, setSiteOptions] = useState<CurtailmentSiteOption[]>([]);
  const [isLoadingSiteOptions, setIsLoadingSiteOptions] = useState(false);
  const [siteOptionsLoadError, setSiteOptionsLoadError] = useState<string | null>(null);
  /* eslint-disable react-hooks/set-state-in-effect -- site catalog fetch mirrors external permission/load state into modal options */
  useEffect(() => {
    if (!canReadSiteCatalog) {
      setLoadedSiteNameById(new Map());
      setSiteOptions([]);
      setIsLoadingSiteOptions(false);
      setSiteOptionsLoadError(null);
      return undefined;
    }

    const abortController = new AbortController();
    setIsLoadingSiteOptions(true);
    setSiteOptionsLoadError(null);
    void listSites({
      signal: abortController.signal,
      onSuccess: (sites) => {
        if (!abortController.signal.aborted) {
          setLoadedSiteNameById(buildSiteNameById(sites));
          setSiteOptions(createSiteOptions(sites));
        }
      },
      onError: (message) => {
        if (!abortController.signal.aborted) {
          setSiteOptionsLoadError(message || "Couldn't load sites.");
        }
      },
      onFinally: () => {
        if (!abortController.signal.aborted) {
          setIsLoadingSiteOptions(false);
        }
      },
    });

    return () => {
      abortController.abort();
    };
  }, [canReadSiteCatalog, listSites]);
  /* eslint-enable react-hooks/set-state-in-effect */
  const siteNameById = canReadSiteCatalog ? loadedSiteNameById : undefined;
  const {
    activeEvent,
    activeEvents,
    activeEventId,
    activeEventFormValues,
    historyEvents,
    isLoading,
    isStarting,
    isUpdating,
    stoppingEventId,
    adminTerminatingEventId,
    loadError,
    startError,
    clearActionErrors,
    updateError,
    stopError,
    adminTerminateError,
    historyCurrentPage,
    historyHasNextPage,
    historyHasPreviousPage,
    historyPageSize,
    historyStatusFilters,
    refreshCurtailment,
    goToHistoryPage,
    setHistoryStatusFilters,
    selectActiveCurtailment,
    startCurtailment,
    dismissTerminalCurtailment,
    updateCurtailment,
    stopCurtailment,
    adminTerminateCurtailment,
    forceReleaseCurtailment,
  } = useCurtailmentApi({ siteNameById });
  const { responseProfiles, listResponseProfiles } = useCurtailmentResponseProfiles(enableManage, { siteNameById });
  const {
    devices: infrastructureDevices,
    isLoading: isLoadingInfrastructureDevices,
    loadError: infrastructureDevicesError,
    listDevices: listInfrastructureDevices,
  } = useInfrastructureDevices(enableManage, undefined, true);
  const retryInfrastructureDevices = useCallback(() => {
    void listInfrastructureDevices().catch(() => {});
  }, [listInfrastructureDevices]);
  const responseProfileOptions = useMemo(
    () => responseProfiles.filter(isEditableResponseProfile).map(createCurtailmentResponseProfileOption),
    [responseProfiles],
  );
  const defaultSiteScope = useMemo(
    () => (canReadSiteCatalog ? getDefaultCurtailmentSiteScope(activeSite, siteOptions) : undefined),
    [activeSite, canReadSiteCatalog, siteOptions],
  );
  const activeEventIds = useMemo(() => activeEvents.map((event) => event.id), [activeEvents]);
  const [modalMode, setModalMode] = useState<CurtailmentStartModalMode | null>(null);
  const [editSession, setEditSession] = useState<EditCurtailmentSession | null>(null);
  const [pendingStopConfirmation, setPendingStopConfirmation] = useState<PendingStopConfirmation | null>(null);
  const [pendingTerminateRecoveryEventId, setPendingTerminateRecoveryEventId] = useState<string | null>(null);
  const [pendingForceReleaseEventId, setPendingForceReleaseEventId] = useState<string | null>(null);
  const [failedTerminateRecoveryEventId, setFailedTerminateRecoveryEventId] = useState<string | null>(null);
  // Abort restore is unlocked per restore cycle: once the active event leaves
  // the restoring state (restore completed, or a recurtail cycle flipped it
  // back to pending/active), a fresh Stop-restore failure is required before
  // the destructive escalation is offered again. Render-time adjustment per
  // react.dev's "adjusting state when a prop changes" pattern.
  const [prevActiveEventState, setPrevActiveEventState] = useState(activeEvent?.state);
  if (activeEvent?.state !== prevActiveEventState) {
    setPrevActiveEventState(activeEvent?.state);
    if (activeEvent && activeEvent.state !== "restoring" && failedTerminateRecoveryEventId === activeEventId) {
      setFailedTerminateRecoveryEventId(null);
    }
  }
  const refreshAbortControllerRef = useRef<AbortController | null>(null);
  const activeRefreshAbortControllerRef = useRef<AbortController | null>(null);
  const manageSelectionAbortControllerRef = useRef<AbortController | null>(null);
  const manageSelectionRequestIdRef = useRef(0);
  const foregroundRefreshInFlightRef = useRef(false);
  const canUseRecovery = enableManage && enableRecover;
  const recoveryStopError = getRecoveryStopErrorMessage(stopError, activeEvent?.isAutomationOwned, canUseRecovery);
  const errorMessage =
    (modalMode === null ? (startError ?? updateError) : null) ?? recoveryStopError ?? adminTerminateError ?? loadError;
  const isInitialLoading = isLoading && !activeEvent && historyEvents.length === 0;
  const isStopConfirmationSubmitting =
    pendingStopConfirmation !== null && stoppingEventId === pendingStopConfirmation.eventId;
  const isTerminateRecoverySubmitting =
    pendingTerminateRecoveryEventId !== null && adminTerminatingEventId === pendingTerminateRecoveryEventId;
  const isForceReleaseSubmitting =
    pendingForceReleaseEventId !== null && adminTerminatingEventId === pendingForceReleaseEventId;
  const isEditingCurtailment = modalMode === "edit";
  const isModalSubmitting = isEditingCurtailment ? isUpdating : isStarting;
  const hasOngoingCurtailment = activeEvents.some((event) => nonTerminalActiveEventStates.has(event.state));
  const hasOngoingHistoryEvent = historyEvents.some((event) => nonTerminalActiveEventStates.has(event.state));
  const shouldPollCurtailment = hasOngoingCurtailment || hasOngoingHistoryEvent;
  const pendingForceReleaseEvent =
    pendingForceReleaseEventId === null
      ? undefined
      : activeEventId === pendingForceReleaseEventId
        ? activeEvent
        : activeEvents.find((event) => event.id === pendingForceReleaseEventId);
  const pendingForceReleaseMode = pendingForceReleaseEvent?.state === "restoring" ? "restore" : "curtailment";
  const canTerminateActiveRecovery = Boolean(
    canUseRecovery && activeEvent && canTerminateRecoveryCurtailmentEvent(activeEvent),
  );
  const didActiveTerminateRecoveryFail = Boolean(
    activeEventId &&
    activeEvent &&
    failedTerminateRecoveryEventId === activeEventId &&
    canTerminateRecoveryCurtailmentEvent(activeEvent),
  );
  const canForceReleaseActiveEvent = Boolean(
    canUseRecovery &&
    activeEventId &&
    activeEvent &&
    canAbortCurtailmentOwnership(activeEvent) &&
    (!canTerminateActiveRecovery || didActiveTerminateRecoveryFail),
  );

  const runAbortableRefresh = useCallback(<T,>(operation: (signal: AbortSignal) => Promise<T>) => {
    activeRefreshAbortControllerRef.current?.abort();
    activeRefreshAbortControllerRef.current = null;
    refreshAbortControllerRef.current?.abort();
    const abortController = new AbortController();
    refreshAbortControllerRef.current = abortController;
    foregroundRefreshInFlightRef.current = true;

    return operation(abortController.signal).finally(() => {
      if (refreshAbortControllerRef.current === abortController) {
        refreshAbortControllerRef.current = null;
        foregroundRefreshInFlightRef.current = false;
      }
    });
  }, []);

  useEffect(() => {
    void runAbortableRefresh((signal) => refreshCurtailment({ signal })).catch(() => {});

    return () => refreshAbortControllerRef.current?.abort();
  }, [refreshCurtailment, runAbortableRefresh]);

  useCurtailmentPolling({
    enabled: true,
    priority: 1,
    intervalMs: shouldPollCurtailment ? activeCurtailmentRefreshIntervalMs : 30_000,
    refresh: async (signal) => {
      if (foregroundRefreshInFlightRef.current) return;
      const controller = new AbortController();
      activeRefreshAbortControllerRef.current = controller;
      const abort = () => controller.abort();
      signal.addEventListener("abort", abort, { once: true });
      try {
        await Promise.all([
          refreshCurtailment({ background: true, signal: controller.signal }),
          enableManage ? listResponseProfiles(controller.signal) : undefined,
        ]);
      } finally {
        signal.removeEventListener("abort", abort);
        if (activeRefreshAbortControllerRef.current === controller) activeRefreshAbortControllerRef.current = null;
      }
    },
  });

  useEffect(
    () => () => {
      manageSelectionAbortControllerRef.current?.abort();
    },
    [],
  );

  const cancelManageSelection = useCallback(() => {
    manageSelectionAbortControllerRef.current?.abort();
    manageSelectionAbortControllerRef.current = null;
    manageSelectionRequestIdRef.current += 1;
  }, []);

  const closeModal = useCallback(() => {
    cancelManageSelection();
    setModalMode(null);
    setEditSession(null);
    clearActionErrors();
  }, [cancelManageSelection, clearActionErrors]);

  const openCreateModal = useCallback(() => {
    void listResponseProfiles().catch(() => {});
    cancelManageSelection();
    setEditSession(null);
    setModalMode("create");
  }, [cancelManageSelection, listResponseProfiles]);

  const openEditModal = useCallback(() => {
    if (!enableManage || !activeEvent || !activeEventId || !activeEventFormValues) {
      return;
    }

    cancelManageSelection();
    setEditSession({
      eventId: activeEventId,
      initialValues: activeEventFormValues,
      preview: createActiveCurtailmentPreview(activeEvent, activeEventFormValues),
    });
    setModalMode("edit");
  }, [activeEvent, activeEventFormValues, activeEventId, enableManage, cancelManageSelection]);

  const openHistoryManageModal = useCallback(
    (event: CurtailmentHistoryEvent) => {
      if (!enableManage) {
        return;
      }

      if (
        event.id === activeEventId &&
        activeEvent &&
        activeEventFormValues &&
        canUpdateCurtailmentEvent(activeEvent)
      ) {
        cancelManageSelection();
        setEditSession({
          eventId: activeEventId,
          initialValues: activeEventFormValues,
          preview: createActiveCurtailmentPreview(activeEvent, activeEventFormValues),
        });
        setModalMode("edit");
        return;
      }

      manageSelectionAbortControllerRef.current?.abort();
      const requestId = manageSelectionRequestIdRef.current + 1;
      manageSelectionRequestIdRef.current = requestId;
      const abortController = new AbortController();
      manageSelectionAbortControllerRef.current = abortController;

      void selectActiveCurtailment(event.id, { signal: abortController.signal })
        .then(({ activeEvent: selectedActiveEvent, activeEventId: selectedActiveEventId, activeEventFormValues }) => {
          if (
            abortController.signal.aborted ||
            manageSelectionRequestIdRef.current !== requestId ||
            selectedActiveEventId !== event.id
          ) {
            return;
          }

          if (
            !selectedActiveEvent ||
            !selectedActiveEventId ||
            !activeEventFormValues ||
            !canUpdateCurtailmentEvent(selectedActiveEvent)
          ) {
            return;
          }

          setEditSession({
            eventId: selectedActiveEventId,
            initialValues: activeEventFormValues,
            preview: createActiveCurtailmentPreview(selectedActiveEvent, activeEventFormValues),
          });
          setModalMode("edit");
        })
        .catch(() => {})
        .finally(() => {
          if (manageSelectionAbortControllerRef.current === abortController) {
            manageSelectionAbortControllerRef.current = null;
          }
        });
    },
    [activeEvent, activeEventFormValues, activeEventId, enableManage, cancelManageSelection, selectActiveCurtailment],
  );

  const selectHistoryActiveEvent = useCallback(
    (event: CurtailmentHistoryEvent) => {
      cancelManageSelection();
      const abortController = new AbortController();
      manageSelectionAbortControllerRef.current = abortController;

      void selectActiveCurtailment(event.id, { signal: abortController.signal })
        .catch(() => {})
        .finally(() => {
          if (manageSelectionAbortControllerRef.current === abortController) {
            manageSelectionAbortControllerRef.current = null;
          }
        });
    },
    [cancelManageSelection, selectActiveCurtailment],
  );

  const openStopConfirmation = useCallback(
    (action: CurtailmentStopConfirmationAction, eventId = activeEventId) => {
      if (!enableManage || !eventId) {
        return;
      }

      cancelManageSelection();
      setPendingStopConfirmation({ action, eventId });
    },
    [activeEventId, enableManage, cancelManageSelection],
  );

  const openTerminateRecoveryConfirmation = useCallback(() => {
    if (!canUseRecovery || !activeEvent || !activeEventId || !canTerminateRecoveryCurtailmentEvent(activeEvent)) {
      return;
    }

    cancelManageSelection();
    setPendingTerminateRecoveryEventId(activeEventId);
  }, [activeEvent, activeEventId, canUseRecovery, cancelManageSelection]);

  const handleStartSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      void startCurtailment(values)
        .then(closeModal)
        .catch(() => {});
    },
    [closeModal, startCurtailment],
  );

  const handleUpdateSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      const editEventId = editSession?.eventId ?? activeEventId;
      if (!editEventId) {
        return;
      }

      void updateCurtailment(editEventId, values, editSession?.initialValues ?? activeEventFormValues ?? undefined)
        .then(closeModal)
        .catch(() => {});
    },
    [activeEventFormValues, activeEventId, closeModal, editSession, updateCurtailment],
  );

  const handleModalSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      if (isEditingCurtailment) {
        handleUpdateSubmit(values);
        return;
      }

      handleStartSubmit(values);
    },
    [handleStartSubmit, handleUpdateSubmit, isEditingCurtailment],
  );

  const handleHistoryStop = useCallback(
    (event: CurtailmentHistoryEvent) => {
      cancelManageSelection();
      return stopCurtailment(event.id);
    },
    [cancelManageSelection, stopCurtailment],
  );

  const handleHistoryPageChange = useCallback(
    (historyPage: number) => {
      void runAbortableRefresh((signal) => goToHistoryPage(historyPage, { signal })).catch(() => {});
    },
    [goToHistoryPage, runAbortableRefresh],
  );

  const handleHistoryStatusFiltersChange = useCallback(
    (stateFilters: CurtailmentEventState[]) => {
      void runAbortableRefresh((signal) => setHistoryStatusFilters(stateFilters, { signal })).catch(() => {});
    },
    [runAbortableRefresh, setHistoryStatusFilters],
  );

  const handleConfirmStop = useCallback(() => {
    if (!enableManage || !pendingStopConfirmation) {
      return;
    }

    const currentEvent = activeEvents.find((event) => event.id === pendingStopConfirmation.eventId);
    if (!currentEvent || !nonTerminalActiveEventStates.has(currentEvent.state)) {
      setPendingStopConfirmation(null);
      return;
    }

    const stopPromise = stopCurtailment(pendingStopConfirmation.eventId);

    void stopPromise.then(() => setPendingStopConfirmation(null)).catch(() => {});
  }, [activeEvents, enableManage, pendingStopConfirmation, stopCurtailment]);

  const handleConfirmTerminateRecovery = useCallback(
    (options: TerminateRecoveryOptions) => {
      if (!canUseRecovery || !pendingTerminateRecoveryEventId) {
        return;
      }

      const eventId = pendingTerminateRecoveryEventId;
      if (eventId !== activeEventId || !activeEvent || !canTerminateRecoveryCurtailmentEvent(activeEvent)) {
        setPendingTerminateRecoveryEventId(null);
        return;
      }

      void adminTerminateCurtailment(eventId, options)
        .then(() => {
          setPendingTerminateRecoveryEventId(null);
          setFailedTerminateRecoveryEventId((currentEventId) => (currentEventId === eventId ? null : currentEventId));
        })
        .catch((error: unknown) => {
          if (shouldOfferForceReleaseAfterTerminateRecoveryError(error)) {
            setFailedTerminateRecoveryEventId(eventId);
          }
        });
    },
    [activeEvent, activeEventId, adminTerminateCurtailment, canUseRecovery, pendingTerminateRecoveryEventId],
  );

  const handleConfirmForceRelease = useCallback(
    (options: ForceReleaseCurtailmentOptions) => {
      if (!canUseRecovery || !pendingForceReleaseEventId) {
        return;
      }

      const eventId = pendingForceReleaseEventId;
      if (eventId !== activeEventId || !activeEvent || !canAbortCurtailmentOwnership(activeEvent)) {
        setPendingForceReleaseEventId(null);
        return;
      }

      void forceReleaseCurtailment(eventId, options)
        .then(() => {
          setPendingForceReleaseEventId(null);
          setFailedTerminateRecoveryEventId((currentEventId) => (currentEventId === eventId ? null : currentEventId));
        })
        .catch(() => {});
    },
    [activeEvent, activeEventId, canUseRecovery, forceReleaseCurtailment, pendingForceReleaseEventId],
  );

  const handleEditStopCurtailment = useCallback(() => {
    const editEventId = editSession?.eventId ?? activeEventId;

    closeModal();
    openStopConfirmation("stopCurtailment", editEventId);
  }, [activeEventId, closeModal, editSession, openStopConfirmation]);
  const handleEditSettings = useCallback(() => {
    navigate("/settings/curtailment");
  }, [navigate]);

  return (
    <section className={clsx("grid gap-6", className)}>
      <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header title="Curtailment" titleSize="text-heading-300" />
        {enableManage ? (
          <div className="flex items-center gap-2 phone:flex-col phone:items-stretch">
            <Button
              variant={variants.secondary}
              size={sizes.base}
              text="Edit settings"
              onClick={handleEditSettings}
              className="phone:w-full"
            />
            <Button
              variant={variants.primary}
              size={sizes.base}
              text="Run curtailment"
              onClick={openCreateModal}
              disabled={isStarting || isUpdating}
              className="phone:w-full"
            />
          </div>
        ) : null}
      </div>

      {errorMessage ? <CurtailmentMessage message={errorMessage} /> : null}

      {isInitialLoading ? (
        <div className="flex justify-center py-12">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <>
          {activeEvent ? (
            <ActiveCurtailmentStatus
              event={activeEvent}
              onDismissRestored={dismissTerminalCurtailment}
              onRequestTerminateRecovery={
                canTerminateActiveRecovery && !didActiveTerminateRecoveryFail
                  ? openTerminateRecoveryConfirmation
                  : undefined
              }
              onRequestForceRelease={
                canForceReleaseActiveEvent ? () => setPendingForceReleaseEventId(activeEventId) : undefined
              }
              onRequestEdit={enableManage && activeEventFormValues ? openEditModal : undefined}
              onRequestRestore={enableManage ? () => openStopConfirmation("restore") : undefined}
              onRequestStop={
                enableManage && !activeEvent.isAutomationOwned ? () => openStopConfirmation("restore") : undefined
              }
            />
          ) : null}

          <CurtailmentHistory
            activeEventId={activeEventId ?? undefined}
            activeEventIds={activeEventIds}
            events={historyEvents}
            pageSize={historyPageSize}
            currentPage={historyCurrentPage}
            hasNextPage={historyHasNextPage}
            hasPreviousPage={historyHasPreviousPage}
            selectedStatusFilters={historyStatusFilters}
            onPageChange={handleHistoryPageChange}
            onStatusFiltersChange={handleHistoryStatusFiltersChange}
            onManageActiveEvent={enableManage ? openHistoryManageModal : undefined}
            onSelectActiveEvent={canUseRecovery ? selectHistoryActiveEvent : undefined}
            onStopActiveEventRequested={enableManage ? cancelManageSelection : undefined}
            onStopActiveEvent={enableManage ? handleHistoryStop : undefined}
          />
        </>
      )}

      {modalMode ? (
        <CurtailmentStartModal
          open
          mode={modalMode}
          initialValues={isEditingCurtailment ? (editSession?.initialValues ?? undefined) : undefined}
          responseProfiles={isEditingCurtailment ? [] : responseProfileOptions}
          siteOptions={siteOptions}
          buildingScopeEnabled={canReadSiteCatalog}
          rackAndGroupScopeEnabled={canReadRackCatalog}
          infrastructureDevices={infrastructureDevices}
          isLoadingInfrastructureDevices={isLoadingInfrastructureDevices}
          infrastructureDevicesError={infrastructureDevicesError}
          onRetryInfrastructureDevices={retryInfrastructureDevices}
          defaultSiteScope={isEditingCurtailment ? undefined : defaultSiteScope}
          siteScopeEnabled={siteOptions.length > 0 || isLoadingSiteOptions}
          isSiteScopeLoading={isLoadingSiteOptions}
          siteScopeDisabledReason={
            canReadSiteCatalog
              ? (siteOptionsLoadError ?? undefined)
              : "Site scope is not available for the current user."
          }
          preview={isEditingCurtailment ? editSession?.preview : undefined}
          onDismiss={closeModal}
          onSubmit={handleModalSubmit}
          onStopCurtailment={isEditingCurtailment ? handleEditStopCurtailment : undefined}
          isSubmitting={isModalSubmitting}
          actionError={isEditingCurtailment ? updateError : startError}
        />
      ) : null}

      {pendingStopConfirmation ? (
        <CurtailmentStopConfirmationDialog
          open
          action={pendingStopConfirmation.action}
          isSubmitting={isStopConfirmationSubmitting}
          onCancel={() => setPendingStopConfirmation(null)}
          onConfirm={handleConfirmStop}
        />
      ) : null}

      {pendingTerminateRecoveryEventId ? (
        <CurtailmentRecoveryTerminateDialog
          open
          error={adminTerminateError}
          isSubmitting={isTerminateRecoverySubmitting}
          onCancel={() => setPendingTerminateRecoveryEventId(null)}
          onConfirm={handleConfirmTerminateRecovery}
        />
      ) : null}

      {pendingForceReleaseEventId ? (
        <CurtailmentForceReleaseDialog
          open
          error={adminTerminateError}
          isSubmitting={isForceReleaseSubmitting}
          mode={pendingForceReleaseMode}
          onCancel={() => setPendingForceReleaseEventId(null)}
          onConfirm={handleConfirmForceRelease}
        />
      ) : null}
    </section>
  );
}

export default CurtailmentManagementPanel;
