import { type ReactElement, type ReactNode, useState } from "react";

import ActiveCurtailmentStatus, {
  type ActiveCurtailmentEvent,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  type ActiveCurtailmentDisplayState,
  getActiveCurtailmentDisplayState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import {
  orderLabels,
  pacingDetail,
  rolloutLifecycleActions,
  rolloutStageLabel,
} from "@/protoFleet/features/rollout/rolloutDisplayUtils";
import RolloutErrorCallout from "@/protoFleet/features/rollout/RolloutErrorCallout";
import RolloutMinersModal, { type RolloutMinerFilter } from "@/protoFleet/features/rollout/RolloutMinersModal";
import RolloutPerformanceStrip from "@/protoFleet/features/rollout/RolloutPerformanceStrip";
import type { RolloutEvent, RolloutMinerRow } from "@/protoFleet/features/rollout/rolloutTypes";
import { Alert, Info, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface ActiveCurtailmentRolloutStatusProps {
  event: ActiveCurtailmentEvent;
  rolloutEvent?: RolloutEvent;
  miners?: RolloutMinerRow[];
  defaultDetailsOpen?: boolean;
  onManage?: () => void;
  onPause?: () => void;
  onResume?: () => void;
  onAbort?: () => void;
  onContinueFromReview?: () => void;
  onRestore?: () => void;
  onRestoreNow?: () => void;
  onStopRestore?: () => void;
  onDismiss?: () => void;
}

interface CurtailmentAction {
  key: string;
  text: string;
  variant: "primary" | "secondary" | "secondaryDanger";
  onClick: () => void;
}

const displayStateLabels: Record<ActiveCurtailmentDisplayState, string> = {
  cancelled: "Cancelled",
  curtailed: "Curtailed",
  curtailing: "Curtailing",
  failed: "Failed",
  pending: "Pending",
  restoreIncomplete: "Restore incomplete",
  restored: "Restored",
  restoring: "Restoring",
};

const frameworkDispatchStates = new Set([
  "inProgress",
  "stabilizingTelemetry",
  "pausedAtPilotGate",
  "pausedAtBatchReview",
  "paused",
]);

function statusIcon(displayState: ActiveCurtailmentDisplayState, rolloutEvent?: RolloutEvent): ReactNode {
  if (rolloutEvent?.state === "pausedAtPilotGate" || rolloutEvent?.state === "pausedAtBatchReview") {
    return <Info className="text-text-primary" />;
  }
  if (displayState === "restoreIncomplete" || displayState === "failed" || displayState === "cancelled") {
    return <Alert className="text-intent-critical-fill" />;
  }
  if (displayState === "curtailed" || displayState === "restored") {
    return <Success className="text-intent-success-fill" />;
  }
  if (rolloutEvent?.state === "paused") {
    return <Alert className="text-core-accent-fill" />;
  }
  return <ProgressCircular indeterminate className="text-core-primary-fill" />;
}

function domainActions(
  displayState: ActiveCurtailmentDisplayState,
  handlers: Pick<
    ActiveCurtailmentRolloutStatusProps,
    "onDismiss" | "onManage" | "onRestore" | "onRestoreNow" | "onStopRestore"
  >,
): CurtailmentAction[] {
  const actions: CurtailmentAction[] = [];
  if (handlers.onManage && ["pending", "curtailing", "curtailed"].includes(displayState)) {
    actions.push({ key: "manage", text: "Manage", variant: "secondary", onClick: handlers.onManage });
  }
  if (displayState === "curtailed" && handlers.onRestore) {
    actions.push({ key: "restore", text: "Restore", variant: "primary", onClick: handlers.onRestore });
  } else if ((displayState === "pending" || displayState === "curtailing") && handlers.onRestoreNow) {
    actions.push({ key: "restore-now", text: "Restore now", variant: "secondary", onClick: handlers.onRestoreNow });
  } else if (displayState === "restoring" && handlers.onStopRestore) {
    actions.push({
      key: "stop-restore",
      text: "Stop restore",
      variant: "secondaryDanger",
      onClick: handlers.onStopRestore,
    });
  } else if ((displayState === "restored" || displayState === "restoreIncomplete") && handlers.onDismiss) {
    actions.push({ key: "dismiss", text: "Dismiss", variant: "secondary", onClick: handlers.onDismiss });
  }
  return actions;
}

function telemetryStabilizingSubtitle(rolloutEvent: RolloutEvent): string {
  const seconds = rolloutEvent.estimatedSecondsRemaining;
  if (!seconds || seconds <= 0) {
    return "Review becomes available after miners stabilize.";
  }
  const minutes = Math.max(Math.ceil(seconds / 60), 1);
  return `Review becomes available in ~${minutes}m.`;
}

function RolloutPlanDetails({ event }: { event: RolloutEvent }): ReactElement {
  const pacing = pacingDetail(event);
  return (
    <div className="mt-5" data-testid="active-curtailment-plan-details">
      <div className="grid gap-x-12 gap-y-5 text-text-primary tablet:grid-cols-4">
        <div className="min-w-0">
          <div className="text-200 text-text-primary-50">{pacing.method}</div>
          <div className="mt-1 text-emphasis-300 break-words text-text-primary">{pacing.value}</div>
        </div>
        {event.strategy === "allAtOnce" ? null : (
          <div className="min-w-0">
            <div className="text-200 text-text-primary-50">Order</div>
            <div className="mt-1 text-emphasis-300 break-words text-text-primary">{orderLabels[event.order]}</div>
          </div>
        )}
      </div>
      {pacing.detail ? <div className="mt-3 text-200 text-text-primary-70">{pacing.detail}</div> : null}
    </div>
  );
}

export default function ActiveCurtailmentRolloutStatus({
  event,
  rolloutEvent,
  miners = [],
  defaultDetailsOpen = false,
  onManage,
  onPause,
  onResume,
  onAbort,
  onContinueFromReview,
  onRestore,
  onRestoreNow,
  onStopRestore,
  onDismiss,
}: ActiveCurtailmentRolloutStatusProps): ReactElement {
  const [minerModalFilter, setMinerModalFilter] = useState<RolloutMinerFilter | null>(null);
  const displayState = getActiveCurtailmentDisplayState(event, { dispatchStartedAsCurtailing: true });
  const usesFrameworkDispatch = Boolean(rolloutEvent && frameworkDispatchStates.has(rolloutEvent.state));
  const isReviewGate = rolloutEvent?.state === "pausedAtPilotGate" || rolloutEvent?.state === "pausedAtBatchReview";
  const statusValue =
    usesFrameworkDispatch && rolloutEvent ? rolloutStageLabel(rolloutEvent) : displayStateLabels[displayState];
  const visibleActions: CurtailmentAction[] = usesFrameworkDispatch
    ? rolloutEvent
      ? rolloutLifecycleActions(rolloutEvent, {
          onManage,
          onPause,
          onResume,
          onCancelRemaining: onAbort,
          onContinueFromReview,
        })
          .filter((action) => action.key !== "cancel")
          .map((action) => ({
            key: action.key,
            text: action.text,
            variant: action.variant === "danger" ? "secondaryDanger" : action.variant,
            onClick: action.onClick ?? (() => undefined),
          }))
      : []
    : domainActions(displayState, { onDismiss, onManage, onRestore, onRestoreNow, onStopRestore });
  const overflowActions: RowAction[] = [];
  const canViewMiners = Boolean(rolloutEvent && miners.length > 0);
  const canAbort = Boolean(onAbort && ["pending", "curtailing", "curtailed", "restoring"].includes(displayState));
  if (canViewMiners) {
    overflowActions.push({
      label: "View miners",
      onClick: () => setMinerModalFilter("all"),
      testId: "active-curtailment-view-miners-action",
    });
  }
  if (usesFrameworkDispatch && onRestoreNow && (displayState === "pending" || displayState === "curtailing")) {
    overflowActions.push({
      label: "Restore now",
      onClick: onRestoreNow,
      showGroupDivider: canViewMiners,
      testId: "active-curtailment-restore-now-action",
    });
  }
  if (canAbort && onAbort) {
    overflowActions.push({
      label: displayState === "restoring" ? "Abort restore" : "Abort curtailment",
      onClick: onAbort,
      danger: true,
      showGroupDivider: true,
      testId: "active-curtailment-abort-action",
    });
  }
  const actionVariant = {
    primary: variants.primary,
    secondary: variants.secondary,
    secondaryDanger: variants.secondaryDanger,
  } as const;
  const actions =
    overflowActions.length > 0 || visibleActions.length > 0 ? (
      <>
        {overflowActions.length > 0 ? (
          <RowActionsMenu
            actions={overflowActions}
            ariaLabel={`More actions for ${event.reason}`}
            popoverTestId="active-curtailment-more-actions-menu"
            testIdPrefix="active-curtailment-more-actions"
            triggerClassName="!h-8 !w-8 !px-0 !py-0"
            triggerVariant={variants.secondary}
          />
        ) : null}
        {visibleActions.map((action) => (
          <Button
            key={action.key}
            variant={actionVariant[action.variant]}
            size={sizes.compact}
            text={action.text}
            onClick={action.onClick}
          />
        ))}
      </>
    ) : undefined;
  const notice = rolloutEvent ? (
    <>
      <RolloutErrorCallout event={rolloutEvent} onReviewErrors={() => setMinerModalFilter("errors")} />
      {rolloutEvent.state === "stabilizingTelemetry" ? (
        <Callout
          className="mt-6"
          intent={intents.information}
          prefixIcon={<Info />}
          testId="active-rollout-telemetry-stabilizing-banner"
          title="Telemetry is stabilizing"
          subtitle={telemetryStabilizingSubtitle(rolloutEvent)}
        />
      ) : null}
    </>
  ) : undefined;
  const supplementalDetails = rolloutEvent ? (
    <>
      <RolloutPlanDetails event={rolloutEvent} />
      <RolloutPerformanceStrip event={rolloutEvent} />
    </>
  ) : undefined;

  return (
    <>
      <ActiveCurtailmentStatus
        event={event}
        rolloutPresentation={{
          statusLabel: "Dispatch status",
          statusValue,
          statusIcon: statusIcon(displayState, rolloutEvent),
          actions,
          notice,
          supplementalDetails,
          defaultDetailsOpen,
          autoExpandDetails: isReviewGate,
        }}
      />
      {rolloutEvent ? (
        <RolloutMinersModal
          key={minerModalFilter ?? "closed"}
          open={minerModalFilter !== null}
          event={rolloutEvent}
          miners={miners}
          initialFilter={minerModalFilter ?? "all"}
          onDismiss={() => setMinerModalFilter(null)}
        />
      ) : null}
    </>
  );
}
