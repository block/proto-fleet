import { type ReactElement, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { useCurtailmentApi } from "@/protoFleet/api/useCurtailmentApi";
import ActiveCurtailmentStatus from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import type { CurtailmentEventState } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CurtailmentHistory, { type CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import CurtailmentStartModal, {
  type CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
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

function CurtailmentManagementPanel({ className }: CurtailmentManagementPanelProps): ReactElement {
  const {
    activeEvent,
    activeEventId,
    historyEvents,
    isLoading,
    isStarting,
    stoppingEventId,
    loadError,
    startError,
    stopError,
    historyCurrentPage,
    historyHasNextPage,
    historyHasPreviousPage,
    historyPageSize,
    historyStatusFilter,
    refreshCurtailment,
    goToHistoryPage,
    setHistoryStatusFilter,
    startCurtailment,
    stopCurtailment,
  } = useCurtailmentApi();
  const [showStartModal, setShowStartModal] = useState(false);
  const [pendingStopConfirmation, setPendingStopConfirmation] = useState<PendingStopConfirmation | null>(null);
  const refreshAbortControllerRef = useRef<AbortController | null>(null);
  const errorMessage = startError ?? stopError ?? loadError;
  const isInitialLoading = isLoading && !activeEvent && historyEvents.length === 0;
  const isStopConfirmationSubmitting =
    pendingStopConfirmation !== null && stoppingEventId === pendingStopConfirmation.eventId;

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

  const openStopConfirmation = useCallback(
    (action: CurtailmentStopConfirmationAction) => {
      if (!activeEventId) {
        return;
      }

      setPendingStopConfirmation({ action, eventId: activeEventId });
    },
    [activeEventId],
  );

  const handleStartSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      void startCurtailment(values)
        .then(() => setShowStartModal(false))
        .catch(() => {});
    },
    [startCurtailment],
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

  const handleHistoryStatusFilterChange = useCallback(
    (stateFilter?: CurtailmentEventState) => {
      void runAbortableRefresh((signal) => setHistoryStatusFilter(stateFilter, { signal })).catch(() => {});
    },
    [runAbortableRefresh, setHistoryStatusFilter],
  );

  const handleConfirmStop = useCallback(() => {
    if (!pendingStopConfirmation) {
      return;
    }

    void stopCurtailment(pendingStopConfirmation.eventId)
      .then(() => setPendingStopConfirmation(null))
      .catch(() => {});
  }, [pendingStopConfirmation, stopCurtailment]);

  return (
    <section className={clsx("grid gap-6", className)}>
      <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
        <Header title="Curtailment" titleSize="text-heading-300" />
        <Button
          variant={variants.primary}
          size={sizes.base}
          text="Plan curtailment"
          onClick={() => setShowStartModal(true)}
          disabled={isStarting}
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
            selectedStatusFilter={historyStatusFilter}
            onPageChange={handleHistoryPageChange}
            onStatusFilterChange={handleHistoryStatusFilterChange}
            onStopActiveEvent={handleHistoryStop}
          />
        </>
      )}

      {showStartModal ? (
        <CurtailmentStartModal
          open
          onDismiss={() => setShowStartModal(false)}
          onSubmit={handleStartSubmit}
          isSubmitting={isStarting}
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
