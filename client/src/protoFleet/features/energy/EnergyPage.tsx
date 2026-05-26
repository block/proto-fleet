import { type ReactElement, type ReactNode, useCallback, useEffect, useState } from "react";

import { notifyCurtailmentChanged } from "@/protoFleet/api/curtailmentNotifications";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import useCurtailmentApi from "@/protoFleet/api/useCurtailmentApi";
import ActiveCurtailmentStatus from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import CurtailmentHistory from "@/protoFleet/features/energy/CurtailmentHistory";
import {
  buildStartCurtailmentRequest,
  buildUpdateCurtailmentRequest,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import CurtailmentStartModal, {
  type CurtailmentSubmitValues,
} from "@/protoFleet/features/energy/CurtailmentStartModal";
import CurtailmentStopConfirmationDialog, {
  type CurtailmentStopConfirmationAction,
} from "@/protoFleet/features/energy/CurtailmentStopConfirmationDialog";
import {
  activeCurtailmentEventStates,
  type CurtailmentActiveEvent,
  type CurtailmentApi,
  type CurtailmentEventState,
} from "@/protoFleet/features/energy/types";
import { MINERS_PAGE_SIZE } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const ACTIVE_CURTAILMENT_POLL_INTERVAL_MS = 30_000;

const activeCurtailmentStateSet = new Set<CurtailmentEventState>(activeCurtailmentEventStates);

interface SectionHeaderProps {
  title: string;
  action?: ReactNode;
  titleSize?: string;
}

interface SubmitCurtailmentOptions {
  submit: () => Promise<unknown>;
  successMessage: string;
  errorMessage: string;
  onSuccess: () => void;
}

interface EnergyPageProps {
  api?: CurtailmentApi;
}

function SectionHeader({ title, action, titleSize = "text-heading-200" }: SectionHeaderProps): ReactElement {
  return (
    <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
      <div className="min-w-0">
        <Header title={title} titleSize={titleSize} />
      </div>
      {action ? <div className="shrink-0 phone:w-full">{action}</div> : null}
    </div>
  );
}

function isRestoredEventState(state: CurtailmentEventState): boolean {
  return state === "completed" || state === "completedWithFailures";
}

function shouldPollActiveEvent(state?: CurtailmentEventState): boolean {
  return state !== undefined && activeCurtailmentStateSet.has(state);
}

function getDisplayedActiveEvent(
  activeEvent: CurtailmentActiveEvent | undefined,
  dismissedRestoredEventIds: Set<string>,
): CurtailmentActiveEvent | undefined {
  if (!activeEvent) {
    return undefined;
  }

  if (isRestoredEventState(activeEvent.state) && dismissedRestoredEventIds.has(activeEvent.id)) {
    return undefined;
  }

  return activeEvent;
}

function createCurtailmentFormValuesFromEvent(event: CurtailmentActiveEvent): CurtailmentSubmitValues {
  const rawEvent = event.rawEvent;
  const rawFixedKw = rawEvent?.modeParams.case === "fixedKw" ? rawEvent.modeParams.value : undefined;
  const rawScope = rawEvent?.scope;
  const formValues: CurtailmentSubmitValues = {
    scopeType: "wholeOrg",
    scopeId: "whole-org",
    deviceSetIds: [],
    deviceIdentifiers: [],
    responseProfileId: "customPlan",
    curtailmentMode: "fixedKwReduction",
    minerSelectionStrategy: "leastEfficientFirst",
    targetKw: String(rawFixedKw?.targetKw ?? event.targetKw ?? event.estimatedReductionKw),
    toleranceKw: String(rawFixedKw?.toleranceKw ?? event.toleranceKw ?? ""),
    priority: event.priority,
    minDurationSec: rawEvent?.minCurtailedDurationSec ? String(rawEvent.minCurtailedDurationSec) : "",
    maxDurationSec: rawEvent?.maxDurationSeconds ? String(rawEvent.maxDurationSeconds) : "",
    restoreBatchSize: String(rawEvent?.restoreBatchSize ?? event.restoreBatchSize),
    restoreIntervalSec: String(event.restoreBatchIntervalSec),
    includeMaintenance: rawEvent?.includeMaintenance ?? false,
    reason: event.reason,
  };

  switch (rawScope?.case) {
    case "deviceIdentifiers":
      return {
        ...formValues,
        scopeType: "explicitMiners",
        scopeId: undefined,
        deviceIdentifiers: rawScope.value.deviceIdentifiers,
      };
    case "deviceSetIds":
      return {
        ...formValues,
        scopeType: "deviceSet",
        scopeId: "groups",
        deviceSetIds: rawScope.value.deviceSetIds,
      };
    default:
      return formValues;
  }
}

function EnergyPage({ api: injectedApi }: EnergyPageProps): ReactElement {
  const liveApi = useCurtailmentApi();
  const {
    activeEvent,
    events,
    isLoading,
    refreshCurtailment,
    startCurtailment,
    stopCurtailment,
    updateCurtailmentEvent,
  } = injectedApi ?? liveApi;
  const [showStartModal, setShowStartModal] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [showStopDialog, setShowStopDialog] = useState(false);
  const [stopDialogAction, setStopDialogAction] = useState<CurtailmentStopConfirmationAction>("stopCurtailment");
  const [showPlanBlockedDialog, setShowPlanBlockedDialog] = useState(false);
  const [isCurtailmentSubmitting, setIsCurtailmentSubmitting] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  const [dismissedRestoredEventIds, setDismissedRestoredEventIds] = useState<Set<string>>(() => new Set());
  const completedCount = events.filter((event) => isRestoredEventState(event.state)).length;
  const displayedActiveEvent = getDisplayedActiveEvent(activeEvent, dismissedRestoredEventIds);
  const displayedLoadError = loadError;
  useEffect(() => {
    let isSubscribed = true;

    void refreshCurtailment()
      .then(() => {
        if (isSubscribed) {
          setLoadError(undefined);
        }
      })
      .catch((error) => {
        if (isSubscribed) {
          setLoadError(getErrorMessage(error, "Failed to load curtailment events."));
        }
      });

    return () => {
      isSubscribed = false;
    };
  }, [refreshCurtailment]);

  useEffect(() => {
    if (!shouldPollActiveEvent(activeEvent?.state)) {
      return;
    }

    const intervalId = window.setInterval(() => {
      void refreshCurtailment()
        .then(() => setLoadError(undefined))
        .catch((error) => setLoadError(getErrorMessage(error, "Failed to refresh curtailment events.")));
    }, ACTIVE_CURTAILMENT_POLL_INTERVAL_MS);

    return () => window.clearInterval(intervalId);
  }, [activeEvent?.state, refreshCurtailment]);

  const handlePlanCurtailment = useCallback(() => {
    if (shouldPollActiveEvent(activeEvent?.state)) {
      setShowPlanBlockedDialog(true);
      return;
    }

    setShowStartModal(true);
  }, [activeEvent?.state]);

  const refreshCurtailmentAfterMutation = useCallback(
    async (fallbackMessage: string) => {
      try {
        await refreshCurtailment();
        setLoadError(undefined);
      } catch (error) {
        setLoadError(getErrorMessage(error, fallbackMessage));
      }
    },
    [refreshCurtailment],
  );

  const submitCurtailment = useCallback(
    async ({ submit, successMessage, errorMessage, onSuccess }: SubmitCurtailmentOptions) => {
      if (isCurtailmentSubmitting) {
        return;
      }

      setIsCurtailmentSubmitting(true);

      try {
        await submit();
        pushToast({
          message: successMessage,
          status: STATUSES.success,
        });
        onSuccess();
        notifyCurtailmentChanged();
        await refreshCurtailmentAfterMutation("Failed to refresh curtailment events.");
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, errorMessage),
          status: STATUSES.error,
        });
      } finally {
        setIsCurtailmentSubmitting(false);
      }
    },
    [isCurtailmentSubmitting, refreshCurtailmentAfterMutation],
  );

  const handleStartSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      void submitCurtailment({
        submit: () => startCurtailment(buildStartCurtailmentRequest(values)),
        successMessage: "Curtailment started.",
        errorMessage: "Failed to start curtailment.",
        onSuccess: () => setShowStartModal(false),
      });
    },
    [startCurtailment, submitCurtailment],
  );

  const handleUpdateSubmit = useCallback(
    (values: CurtailmentSubmitValues) => {
      if (!activeEvent) {
        return;
      }

      void submitCurtailment({
        submit: () => updateCurtailmentEvent(buildUpdateCurtailmentRequest(activeEvent.id, values)),
        successMessage: "Curtailment updated.",
        errorMessage: "Failed to update curtailment.",
        onSuccess: () => setShowEditModal(false),
      });
    },
    [activeEvent, submitCurtailment, updateCurtailmentEvent],
  );

  const handleStop = useCallback(
    async ({ rethrow = false }: { rethrow?: boolean } = {}) => {
      if (!activeEvent) {
        return;
      }

      try {
        await stopCurtailment(activeEvent.id);
        pushToast({
          message: "Curtailment restore started.",
          status: STATUSES.success,
        });
        setShowStopDialog(false);
        setShowEditModal(false);
        notifyCurtailmentChanged();
        await refreshCurtailmentAfterMutation("Failed to refresh curtailment events.");
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to stop curtailment."),
          status: STATUSES.error,
        });

        if (rethrow) {
          throw error;
        }
      }
    },
    [activeEvent, refreshCurtailmentAfterMutation, stopCurtailment],
  );

  const openStopDialog = useCallback((action: CurtailmentStopConfirmationAction = "stopCurtailment") => {
    setStopDialogAction(action);
    setShowStopDialog(true);
  }, []);

  const handleDismissRestored = useCallback(() => {
    if (!activeEvent) {
      return;
    }

    setDismissedRestoredEventIds((current) => {
      const nextDismissedEventIds = new Set(current);
      nextDismissedEventIds.add(activeEvent.id);
      return nextDismissedEventIds;
    });
  }, [activeEvent]);

  if (isLoading && events.length === 0) {
    return (
      <div className="flex justify-center py-20">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <div className="flex min-h-full flex-col gap-16 bg-surface-base px-6 py-8 laptop:px-10 laptop:py-10">
      <SectionHeader
        title="Energy"
        titleSize="text-heading-400"
        action={
          <Button
            variant={variants.primary}
            size={sizes.base}
            text="Plan curtailment"
            onClick={handlePlanCurtailment}
            className="phone:w-full"
          />
        }
      />

      {displayedLoadError ? (
        <div className="-mt-8 flex items-center gap-4 rounded-2xl bg-surface-elevated-base p-5 text-300 text-text-primary shadow-50">
          <Alert className="text-intent-warning-fill" />
          <span className="text-emphasis-300">{displayedLoadError}</span>
        </div>
      ) : null}

      {displayedActiveEvent ? (
        <ActiveCurtailmentStatus
          event={displayedActiveEvent}
          onDismissRestored={handleDismissRestored}
          onRequestEdit={() => setShowEditModal(true)}
          onRequestRestore={() => openStopDialog("restore")}
          onRequestStop={() => openStopDialog("stopCurtailment")}
        />
      ) : null}

      <CurtailmentHistory
        activeEventId={displayedActiveEvent?.id}
        events={events}
        pageSize={MINERS_PAGE_SIZE}
        onManageActiveEvent={() => setShowEditModal(true)}
        onStopActiveEvent={() => handleStop({ rethrow: true })}
      />

      {showStartModal ? (
        <CurtailmentStartModal
          open={showStartModal}
          onDismiss={() => setShowStartModal(false)}
          onSubmit={handleStartSubmit}
          isSubmitting={isCurtailmentSubmitting}
        />
      ) : null}

      {activeEvent && showEditModal ? (
        <CurtailmentStartModal
          key={activeEvent.id}
          open
          mode="edit"
          initialValues={createCurtailmentFormValuesFromEvent(activeEvent)}
          onDismiss={() => setShowEditModal(false)}
          onSubmit={handleUpdateSubmit}
          onStopCurtailment={() => openStopDialog("stopCurtailment")}
          isSubmitting={isCurtailmentSubmitting}
        />
      ) : null}

      <CurtailmentStopConfirmationDialog
        open={showStopDialog}
        action={stopDialogAction}
        onCancel={() => setShowStopDialog(false)}
        onConfirm={() => void handleStop()}
      />

      <Dialog
        open={showPlanBlockedDialog}
        title="Curtailment already in progress"
        onDismiss={() => setShowPlanBlockedDialog(false)}
        icon={
          <DialogIcon intent="warning">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Got it",
            variant: variants.primary,
            onClick: () => setShowPlanBlockedDialog(false),
          },
        ]}
      >
        <div className="text-300 text-text-primary-70">
          You can't plan a curtailment while another curtailment is active or restoring.
        </div>
      </Dialog>

      <div className="sr-only" aria-live="polite">
        {completedCount.toLocaleString()} completed curtailment events.
      </div>
    </div>
  );
}

export default EnergyPage;
