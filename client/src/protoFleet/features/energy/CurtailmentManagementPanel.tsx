import { type ReactElement, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import ActiveCurtailmentStatus, {
  type ActiveCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentEventState } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CurtailmentHistory, { type CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import CurtailmentStartModal, {
  type CurtailmentPlanPreview,
  type CurtailmentStartModalMode,
  type CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import { createCurtailmentPlanPreview } from "@/protoFleet/features/energy/useCurtailmentPlanPreview";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface CurtailmentManagementPanelProps {
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

function CurtailmentManagementPanel({ className }: CurtailmentManagementPanelProps): ReactElement {
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
  const [modalMode, setModalMode] = useState<CurtailmentStartModalMode | null>(null);
  const [editSession, setEditSession] = useState<EditCurtailmentSession | null>(null);
  const [pendingStopConfirmation, setPendingStopConfirmation] = useState<PendingStopConfirmation | null>(null);
  const refreshAbortControllerRef = useRef<AbortController | null>(null);
  const errorMessage = startError ?? updateError ?? stopError ?? loadError;
  const isInitialLoading = isLoading && !activeEvent && historyEvents.length === 0;
  const isStopConfirmationSubmitting =
    pendingStopConfirmation !== null && stoppingEventId === pendingStopConfirmation.eventId;
  const isEditingCurtailment = modalMode === "edit";
  const isModalSubmitting = isEditingCurtailment ? isUpdating : isStarting;

  const runAbortableRefresh = useCallback(<T,>(operation: (signal: AbortSignal) => Promise<T>) => {
    refreshAbortControllerRef.current?.abort();
    const abortController = new AbortController();
    refreshAbortControllerRef.current = abortController;

    return operation(abortController.signal).finally(() => {
      if (refreshAbortControllerRef.current === abortController) {
        refreshAbortControllerRef.current = null;
      }
    });
  }, []);

  useEffect(() => {
    void runAbortableRefresh((signal) => refreshCurtailment({ signal })).catch(() => {});

    return () => refreshAbortControllerRef.current?.abort();
  }, [refreshCurtailment, runAbortableRefresh]);

  const closeModal = useCallback(() => {
    setModalMode(null);
    setEditSession(null);
  }, []);

  const openCreateModal = useCallback(() => {
    setEditSession(null);
    setModalMode("create");
  }, []);

  const openEditModal = useCallback(() => {
    if (!activeEvent || !activeEventId || !activeEventFormValues) {
      return;
    }

    setEditSession({
      eventId: activeEventId,
      initialValues: activeEventFormValues,
      preview: createActiveCurtailmentPreview(activeEvent, activeEventFormValues),
    });
    setModalMode("edit");
  }, [activeEvent, activeEventFormValues, activeEventId]);

  const openStopConfirmation = useCallback(
    (action: CurtailmentStopConfirmationAction, eventId = activeEventId) => {
      if (!eventId) {
        return;
      }

      setPendingStopConfirmation({ action, eventId });
    },
    [activeEventId],
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
    if (!pendingStopConfirmation) {
      return;
    }

    void stopCurtailment(pendingStopConfirmation.eventId)
      .then(() => setPendingStopConfirmation(null))
      .catch(() => {});
  }, [pendingStopConfirmation, stopCurtailment]);

  const handleEditStopCurtailment = useCallback(() => {
    const editEventId = editSession?.eventId ?? activeEventId;

    closeModal();
    openStopConfirmation("stopCurtailment", editEventId);
  }, [activeEventId, closeModal, editSession, openStopConfirmation]);

  return (
    <section className={clsx("grid gap-6", className)}>
      <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header title="Curtailment" titleSize="text-heading-300" />
        <Button
          variant={variants.primary}
          size={sizes.base}
          text="Plan curtailment"
          onClick={openCreateModal}
          disabled={isStarting || isUpdating}
          className="phone:w-full"
        />
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
              onRequestEdit={openEditModal}
              onRequestRestore={() => openStopConfirmation("restore")}
              onRequestStop={() => openStopConfirmation("stopCurtailment")}
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
            onStopActiveEvent={handleHistoryStop}
          />
        </>
      )}

      {modalMode ? (
        <CurtailmentStartModal
          open
          mode={modalMode}
          initialValues={isEditingCurtailment ? (editSession?.initialValues ?? undefined) : undefined}
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
    </section>
  );
}

export default CurtailmentManagementPanel;
