import { type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";

import {
  type AdminTerminateCurtailmentOptions,
  type AdminTerminateCurtailmentState,
  adminTerminateReasonRequiredMessage,
  useCurtailmentApi,
} from "@/protoFleet/api/useCurtailmentApi";
import useCurtailmentResponseProfiles from "@/protoFleet/api/useCurtailmentResponseProfiles";
import ActiveCurtailmentStatus, {
  type ActiveCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentEventState } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CurtailmentHistory, { type CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import CurtailmentStartModal, {
  type CurtailmentPlanPreview,
  type CurtailmentResponseProfileOption,
  type CurtailmentStartModalMode,
  type CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { createCurtailmentPlanPreview } from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import type {
  ResponseProfile,
  ResponseProfileFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Radio from "@/shared/components/Radio";
import Textarea from "@/shared/components/Textarea";

interface CurtailmentManagementPanelProps {
  canAdminRecoverCurtailment?: boolean;
  canManageCurtailment?: boolean;
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

interface AdminTerminateDialogProps {
  error?: string | null;
  isSubmitting?: boolean;
  onCancel: () => void;
  onConfirm: (options: AdminTerminateCurtailmentOptions) => void;
  open: boolean;
}

const activeCurtailmentRefreshIntervalMs = 3_000;
const nonTerminalActiveEventStates = new Set<CurtailmentEventState>(["pending", "active", "restoring"]);
const updateableCurtailmentEventStates = new Set<CurtailmentEventState>(["pending", "active"]);
const forceRestorableCurtailmentEventStates = new Set<CurtailmentEventState>(["pending", "active"]);
const defaultResponseDeadlineMinutes = "15";
const defaultMaxDurationSec = "900";
const immediateRestoreBatchSize = "10000";

const adminTerminateStateOptions: { label: string; value: AdminTerminateCurtailmentState }[] = [
  { label: "Cancelled", value: "cancelled" },
  { label: "Failed", value: "failed" },
];

function minutesToSeconds(value: string): string {
  const minutes = Number(value);

  if (!Number.isFinite(minutes) || minutes <= 0) {
    return defaultMaxDurationSec;
  }

  return String(minutes * 60);
}

function createResponseProfileFormValuesFromProfile(profile: ResponseProfile): ResponseProfileFormValues {
  if (profile.formValues) {
    const siteId = profile.formValues.siteId.trim();

    return {
      ...profile.formValues,
      deviceIdentifiers: [],
      siteId,
      siteName: siteId ? profile.formValues.siteName.trim() : "",
    };
  }

  const targetKwMatch = profile.targetSummary.match(/(\d+(?:\.\d+)?)/);
  const actionType: ResponseProfileFormValues["actionType"] = targetKwMatch ? "fixedKwReduction" : "fullFleet";
  const responseDeadlineMinutes = profile.deadlineSummary.match(/(\d+)/)?.[1] ?? defaultResponseDeadlineMinutes;

  return {
    name: profile.name,
    actionType,
    targetKw: targetKwMatch?.[1] ?? "",
    deviceIdentifiers: [],
    siteId: "",
    siteName: "",
    selectionStrategy: "leastEfficientFirst",
    restoreBehavior: profile.restoreBehavior.toLowerCase().includes("immediate")
      ? "automaticImmediateRestore"
      : "automaticBatchRestore",
    minDurationSec: "",
    maxDurationSec: minutesToSeconds(responseDeadlineMinutes),
    curtailBatchSize: "",
    curtailBatchIntervalSec: "",
    restoreBatchSize: profile.restoreBehavior.toLowerCase().includes("immediate") ? immediateRestoreBatchSize : "",
    restoreIntervalSec: "",
    responseDeadlineMinutes,
    includeMaintenance: true,
  };
}

function createCurtailmentResponseProfileOption(profile: ResponseProfile): CurtailmentResponseProfileOption {
  const values = createResponseProfileFormValuesFromProfile(profile);
  const restoreBatchSize =
    values.restoreBatchSize ||
    (values.restoreBehavior === "automaticImmediateRestore" ? immediateRestoreBatchSize : "");
  const siteId = values.siteId.trim();
  const siteName = siteId ? values.siteName || `Site ${siteId}` : "";

  return {
    id: profile.id,
    label: profile.name,
    values: {
      scopeType: siteId ? "site" : "wholeOrg",
      scopeId: siteId ? siteName : "whole-org",
      siteId,
      deviceSetIds: [],
      deviceIdentifiers: [],
      curtailmentMode: values.actionType,
      minerSelectionStrategy: values.selectionStrategy,
      targetKw: values.targetKw,
      curtailBatchSize: values.curtailBatchSize,
      curtailBatchIntervalSec: values.curtailBatchIntervalSec,
      restoreBatchSize,
      restoreIntervalSec: values.restoreIntervalSec,
      includeMaintenance: values.includeMaintenance,
    },
  };
}

function CurtailmentMessage({ message }: CurtailmentMessageProps): ReactElement {
  return (
    <div className="flex items-center gap-3 rounded-lg bg-intent-warning-10 px-4 py-3 text-300 text-text-primary">
      <Alert className="shrink-0 text-intent-warning-fill" />
      <span className="text-emphasis-300">{message}</span>
    </div>
  );
}

function CurtailmentAdminTerminateDialog({
  error,
  isSubmitting = false,
  onCancel,
  onConfirm,
  open,
}: AdminTerminateDialogProps): ReactElement {
  const [targetState, setTargetState] = useState<AdminTerminateCurtailmentState>("cancelled");
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState<string | null>(null);
  const validationError = reasonError ?? error ?? null;

  const confirmTerminate = useCallback(() => {
    const trimmedReason = reason.trim();
    if (!trimmedReason) {
      setReasonError(adminTerminateReasonRequiredMessage);
      return;
    }

    setReasonError(null);
    onConfirm({ reason: trimmedReason, targetState });
  }, [onConfirm, reason, targetState]);

  return (
    <Dialog
      open={open}
      title="Admin terminate event?"
      onDismiss={onCancel}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onCancel,
          disabled: isSubmitting,
        },
        {
          text: "Terminate event",
          variant: variants.danger,
          onClick: confirmTerminate,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="grid gap-4 text-300 text-text-primary">
        <p className="text-text-primary-70">
          Only terminate after restore has started. This closes the event audit trail as cancelled or failed.
        </p>
        <fieldset className="grid gap-2">
          <legend className="text-emphasis-300">Target state</legend>
          <div className="flex flex-wrap gap-4">
            {adminTerminateStateOptions.map((option) => (
              <label key={option.value} className="flex items-center gap-2">
                <Radio
                  name="admin-terminate-target-state"
                  value={option.value}
                  selected={targetState === option.value}
                  onChange={() => setTargetState(option.value)}
                  disabled={isSubmitting}
                />
                <span>{option.label}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <Textarea
          id="admin-terminate-reason"
          label="Reason"
          initValue={reason}
          rows={3}
          maxLength={256}
          required
          error={validationError ?? false}
          onChange={(value) => {
            setReason(value);
            if (value.trim()) {
              setReasonError(null);
            }
          }}
        />
      </div>
    </Dialog>
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

function canForceRestoreCurtailmentEvent(event: ActiveCurtailmentEvent): boolean {
  return Boolean(event.isAutomationOwned && forceRestorableCurtailmentEventStates.has(event.state));
}

function canAdminTerminateCurtailmentEvent(event: Pick<ActiveCurtailmentEvent, "state">): boolean {
  return event.state === "restoring";
}

function CurtailmentManagementPanel({
  canAdminRecoverCurtailment = false,
  canManageCurtailment = true,
  className,
}: CurtailmentManagementPanelProps): ReactElement {
  const navigate = useNavigate();
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
  } = useCurtailmentApi();
  const { responseProfiles } = useCurtailmentResponseProfiles(canManageCurtailment);
  const responseProfileOptions = useMemo(
    () => responseProfiles.map(createCurtailmentResponseProfileOption),
    [responseProfiles],
  );
  const activeEventIds = useMemo(() => activeEvents.map((event) => event.id), [activeEvents]);
  const [modalMode, setModalMode] = useState<CurtailmentStartModalMode | null>(null);
  const [editSession, setEditSession] = useState<EditCurtailmentSession | null>(null);
  const [pendingStopConfirmation, setPendingStopConfirmation] = useState<PendingStopConfirmation | null>(null);
  const [pendingAdminTerminateEventId, setPendingAdminTerminateEventId] = useState<string | null>(null);
  const refreshAbortControllerRef = useRef<AbortController | null>(null);
  const activeRefreshAbortControllerRef = useRef<AbortController | null>(null);
  const manageSelectionAbortControllerRef = useRef<AbortController | null>(null);
  const manageSelectionRequestIdRef = useRef(0);
  const foregroundRefreshInFlightRef = useRef(false);
  const canUseAdminRecovery = canManageCurtailment && canAdminRecoverCurtailment;
  const recoveryStopError =
    stopError && activeEvent?.isAutomationOwned
      ? `${stopError} ${
          canUseAdminRecovery
            ? "Use Force restore to override active automation demand and minimum-duration guards."
            : "Ask an admin to force restore if automation demand remains asserted or the source is stale."
        }`
      : stopError;
  const errorMessage = startError ?? updateError ?? recoveryStopError ?? adminTerminateError ?? loadError;
  const isInitialLoading = isLoading && !activeEvent && historyEvents.length === 0;
  const isStopConfirmationSubmitting =
    pendingStopConfirmation !== null && stoppingEventId === pendingStopConfirmation.eventId;
  const isAdminTerminateSubmitting =
    pendingAdminTerminateEventId !== null && adminTerminatingEventId === pendingAdminTerminateEventId;
  const isEditingCurtailment = modalMode === "edit";
  const isModalSubmitting = isEditingCurtailment ? isUpdating : isStarting;
  const hasOngoingCurtailment = activeEvents.some((event) => nonTerminalActiveEventStates.has(event.state));
  const hasOngoingHistoryEvent = historyEvents.some((event) => nonTerminalActiveEventStates.has(event.state));
  const shouldPollCurtailment = hasOngoingCurtailment || hasOngoingHistoryEvent;

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

  useEffect(() => {
    if (!shouldPollCurtailment) {
      return undefined;
    }

    const refreshActiveCurtailment = (): void => {
      if (
        foregroundRefreshInFlightRef.current ||
        refreshAbortControllerRef.current ||
        activeRefreshAbortControllerRef.current
      ) {
        return;
      }

      const abortController = new AbortController();
      activeRefreshAbortControllerRef.current = abortController;

      void refreshCurtailment({ background: true, signal: abortController.signal })
        .catch(() => {})
        .finally(() => {
          if (activeRefreshAbortControllerRef.current === abortController) {
            activeRefreshAbortControllerRef.current = null;
          }
        });
    };

    const intervalId = window.setInterval(() => {
      refreshActiveCurtailment();
    }, activeCurtailmentRefreshIntervalMs);

    return () => {
      window.clearInterval(intervalId);
      activeRefreshAbortControllerRef.current?.abort();
      activeRefreshAbortControllerRef.current = null;
    };
  }, [refreshCurtailment, shouldPollCurtailment]);

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
  }, [cancelManageSelection]);

  const openCreateModal = useCallback(() => {
    cancelManageSelection();
    setEditSession(null);
    setModalMode("create");
  }, [cancelManageSelection]);

  const openEditModal = useCallback(() => {
    if (!canManageCurtailment || !activeEvent || !activeEventId || !activeEventFormValues) {
      return;
    }

    cancelManageSelection();
    setEditSession({
      eventId: activeEventId,
      initialValues: activeEventFormValues,
      preview: createActiveCurtailmentPreview(activeEvent, activeEventFormValues),
    });
    setModalMode("edit");
  }, [activeEvent, activeEventFormValues, activeEventId, canManageCurtailment, cancelManageSelection]);

  const openHistoryManageModal = useCallback(
    (event: CurtailmentHistoryEvent) => {
      if (!canManageCurtailment) {
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
    [
      activeEvent,
      activeEventFormValues,
      activeEventId,
      canManageCurtailment,
      cancelManageSelection,
      selectActiveCurtailment,
    ],
  );

  const openStopConfirmation = useCallback(
    (action: CurtailmentStopConfirmationAction, eventId = activeEventId) => {
      if (!canManageCurtailment || !eventId) {
        return;
      }

      cancelManageSelection();
      setPendingStopConfirmation({ action, eventId });
    },
    [activeEventId, canManageCurtailment, cancelManageSelection],
  );

  const openAdminTerminateConfirmation = useCallback(() => {
    if (!canUseAdminRecovery || !activeEvent || !activeEventId || !canAdminTerminateCurtailmentEvent(activeEvent)) {
      return;
    }

    cancelManageSelection();
    setPendingAdminTerminateEventId(activeEventId);
  }, [activeEvent, activeEventId, canUseAdminRecovery, cancelManageSelection]);

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
    if (!canManageCurtailment || !pendingStopConfirmation) {
      return;
    }

    const force = pendingStopConfirmation.action === "forceRestore";
    if (
      force &&
      (!canUseAdminRecovery ||
        pendingStopConfirmation.eventId !== activeEventId ||
        !activeEvent ||
        !canForceRestoreCurtailmentEvent(activeEvent))
    ) {
      setPendingStopConfirmation(null);
      return;
    }

    const currentEvent = activeEvents.find((event) => event.id === pendingStopConfirmation.eventId);
    if (!currentEvent || !nonTerminalActiveEventStates.has(currentEvent.state)) {
      setPendingStopConfirmation(null);
      return;
    }

    const stopPromise = force
      ? stopCurtailment(pendingStopConfirmation.eventId, { force: true })
      : stopCurtailment(pendingStopConfirmation.eventId);

    void stopPromise.then(() => setPendingStopConfirmation(null)).catch(() => {});
  }, [
    activeEvent,
    activeEventId,
    activeEvents,
    canUseAdminRecovery,
    canManageCurtailment,
    pendingStopConfirmation,
    stopCurtailment,
  ]);

  const handleConfirmAdminTerminate = useCallback(
    (options: AdminTerminateCurtailmentOptions) => {
      if (!canUseAdminRecovery || !pendingAdminTerminateEventId) {
        return;
      }

      const currentEvent =
        pendingAdminTerminateEventId === activeEventId
          ? activeEvent
          : activeEvents.find((event) => event.id === pendingAdminTerminateEventId);
      if (!currentEvent || !canAdminTerminateCurtailmentEvent(currentEvent)) {
        setPendingAdminTerminateEventId(null);
        return;
      }

      void adminTerminateCurtailment(pendingAdminTerminateEventId, options)
        .then(() => setPendingAdminTerminateEventId(null))
        .catch(() => {});
    },
    [
      activeEvent,
      activeEventId,
      activeEvents,
      adminTerminateCurtailment,
      canUseAdminRecovery,
      pendingAdminTerminateEventId,
    ],
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
        {canManageCurtailment ? (
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
              onRequestAdminTerminate={
                canUseAdminRecovery && canAdminTerminateCurtailmentEvent(activeEvent)
                  ? openAdminTerminateConfirmation
                  : undefined
              }
              onRequestEdit={canManageCurtailment ? openEditModal : undefined}
              onRequestForceRestore={
                canUseAdminRecovery && canForceRestoreCurtailmentEvent(activeEvent)
                  ? () => openStopConfirmation("forceRestore")
                  : undefined
              }
              onRequestRestore={canManageCurtailment ? () => openStopConfirmation("restore") : undefined}
              onRequestStop={canManageCurtailment ? () => openStopConfirmation("stopCurtailment") : undefined}
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
            onManageActiveEvent={canManageCurtailment ? openHistoryManageModal : undefined}
            onStopActiveEventRequested={canManageCurtailment ? cancelManageSelection : undefined}
            onStopActiveEvent={canManageCurtailment ? handleHistoryStop : undefined}
          />
        </>
      )}

      {modalMode ? (
        <CurtailmentStartModal
          open
          mode={modalMode}
          initialValues={isEditingCurtailment ? (editSession?.initialValues ?? undefined) : undefined}
          responseProfiles={isEditingCurtailment ? [] : responseProfileOptions}
          preview={isEditingCurtailment ? editSession?.preview : undefined}
          onDismiss={closeModal}
          onSubmit={handleModalSubmit}
          onStopCurtailment={isEditingCurtailment ? handleEditStopCurtailment : undefined}
          isSubmitting={isModalSubmitting}
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

      {pendingAdminTerminateEventId ? (
        <CurtailmentAdminTerminateDialog
          open
          error={adminTerminateError}
          isSubmitting={isAdminTerminateSubmitting}
          onCancel={() => setPendingAdminTerminateEventId(null)}
          onConfirm={handleConfirmAdminTerminate}
        />
      ) : null}
    </section>
  );
}

export default CurtailmentManagementPanel;
