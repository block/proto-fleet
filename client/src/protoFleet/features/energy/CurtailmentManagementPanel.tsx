import { type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";

import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
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

interface CurtailmentManagementPanelProps {
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

const activeCurtailmentRefreshIntervalMs = 3_000;
const nonTerminalActiveEventStates = new Set<CurtailmentEventState>(["pending", "active", "restoring"]);
const defaultResponseDeadlineMinutes = "15";
const defaultMaxDurationSec = "900";
const immediateRestoreBatchSize = "10000";

function minutesToSeconds(value: string): string {
  const minutes = Number(value);

  if (!Number.isFinite(minutes) || minutes <= 0) {
    return defaultMaxDurationSec;
  }

  return String(minutes * 60);
}

function createResponseProfileFormValuesFromProfile(profile: ResponseProfile): ResponseProfileFormValues {
  if (profile.formValues) {
    return {
      ...profile.formValues,
      deviceIdentifiers: [],
      siteId: "",
      siteName: "",
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

  return {
    id: profile.id,
    label: profile.name,
    values: {
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

function CurtailmentManagementPanel({
  canManageCurtailment = true,
  className,
}: CurtailmentManagementPanelProps): ReactElement {
  const navigate = useNavigate();
  const {
    activeEvent,
    activeEventId,
    activeEventFormValues,
    historyEvents,
    isLoading,
    isStarting,
    isUpdating,
    stoppingEventId,
    loadError,
    startError,
    updateError,
    stopError,
    historyCurrentPage,
    historyHasNextPage,
    historyHasPreviousPage,
    historyPageSize,
    historyStatusFilters,
    refreshCurtailment,
    goToHistoryPage,
    setHistoryStatusFilters,
    startCurtailment,
    dismissTerminalCurtailment,
    updateCurtailment,
    stopCurtailment,
  } = useCurtailmentApi();
  const { responseProfiles } = useCurtailmentResponseProfiles(canManageCurtailment);
  const responseProfileOptions = useMemo(
    () => responseProfiles.map(createCurtailmentResponseProfileOption),
    [responseProfiles],
  );
  const [modalMode, setModalMode] = useState<CurtailmentStartModalMode | null>(null);
  const [editSession, setEditSession] = useState<EditCurtailmentSession | null>(null);
  const [pendingStopConfirmation, setPendingStopConfirmation] = useState<PendingStopConfirmation | null>(null);
  const [showActiveCurtailmentDialog, setShowActiveCurtailmentDialog] = useState(false);
  const refreshAbortControllerRef = useRef<AbortController | null>(null);
  const activeRefreshAbortControllerRef = useRef<AbortController | null>(null);
  const foregroundRefreshInFlightRef = useRef(false);
  const errorMessage = startError ?? updateError ?? stopError ?? loadError;
  const isInitialLoading = isLoading && !activeEvent && historyEvents.length === 0;
  const isStopConfirmationSubmitting =
    pendingStopConfirmation !== null && stoppingEventId === pendingStopConfirmation.eventId;
  const isEditingCurtailment = modalMode === "edit";
  const isModalSubmitting = isEditingCurtailment ? isUpdating : isStarting;
  const activeEventState = activeEvent?.state;
  const hasOngoingCurtailment = activeEventState ? nonTerminalActiveEventStates.has(activeEventState) : false;

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
    if (!hasOngoingCurtailment) {
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
  }, [hasOngoingCurtailment, refreshCurtailment]);

  const closeModal = useCallback(() => {
    setModalMode(null);
    setEditSession(null);
  }, []);

  const openCreateModal = useCallback(() => {
    if (hasOngoingCurtailment) {
      setShowActiveCurtailmentDialog(true);
      return;
    }

    setEditSession(null);
    setModalMode("create");
  }, [hasOngoingCurtailment]);

  const openEditModal = useCallback(() => {
    if (!canManageCurtailment || !activeEvent || !activeEventId || !activeEventFormValues) {
      return;
    }

    setEditSession({
      eventId: activeEventId,
      initialValues: activeEventFormValues,
      preview: createActiveCurtailmentPreview(activeEvent, activeEventFormValues),
    });
    setModalMode("edit");
  }, [activeEvent, activeEventFormValues, activeEventId, canManageCurtailment]);

  const openStopConfirmation = useCallback(
    (action: CurtailmentStopConfirmationAction, eventId = activeEventId) => {
      if (!canManageCurtailment || !eventId) {
        return;
      }

      setPendingStopConfirmation({ action, eventId });
    },
    [activeEventId, canManageCurtailment],
  );

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
    (event: CurtailmentHistoryEvent) => stopCurtailment(event.id),
    [stopCurtailment],
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

    void stopCurtailment(pendingStopConfirmation.eventId)
      .then(() => setPendingStopConfirmation(null))
      .catch(() => {});
  }, [canManageCurtailment, pendingStopConfirmation, stopCurtailment]);

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
              onRequestEdit={canManageCurtailment ? openEditModal : undefined}
              onRequestRestore={canManageCurtailment ? () => openStopConfirmation("restore") : undefined}
              onRequestStop={canManageCurtailment ? () => openStopConfirmation("stopCurtailment") : undefined}
            />
          ) : null}

          <CurtailmentHistory
            activeEventId={activeEventId ?? undefined}
            events={historyEvents}
            pageSize={historyPageSize}
            currentPage={historyCurrentPage}
            hasNextPage={historyHasNextPage}
            hasPreviousPage={historyHasPreviousPage}
            selectedStatusFilters={historyStatusFilters}
            onPageChange={handleHistoryPageChange}
            onStatusFiltersChange={handleHistoryStatusFiltersChange}
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

      {showActiveCurtailmentDialog ? (
        <Dialog
          open
          title="Curtailment already active"
          testId="active-curtailment-limit-dialog"
          onDismiss={() => setShowActiveCurtailmentDialog(false)}
          icon={
            <DialogIcon intent="warning">
              <Alert />
            </DialogIcon>
          }
          buttons={[
            {
              text: "Got it",
              variant: variants.primary,
              onClick: () => setShowActiveCurtailmentDialog(false),
            },
          ]}
        >
          <div className="text-300 text-text-primary-70">You can only have one active curtailment at a time.</div>
        </Dialog>
      ) : null}
    </section>
  );
}

export default CurtailmentManagementPanel;
