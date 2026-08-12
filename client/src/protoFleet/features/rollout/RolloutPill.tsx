import type { ReactElement } from "react";
import { Link } from "react-router-dom";

import {
  pacingSummary,
  phaseLabel,
  rolloutActionNoun,
  rolloutPhaseCount,
  rolloutProcessLabel,
} from "./rolloutDisplayUtils";
import type { RolloutEvent } from "./rolloutTypes";
import PageHeaderPopoverPill from "@/protoFleet/components/PageHeader/PageHeaderPopoverPill";

interface RolloutPillProps {
  event: RolloutEvent;
  /** Route to the process detail; renders the "View {action}" link when set and
   * no `onViewRollout` handler is provided. */
  detailsPath?: string;
  /** When set, "View {action}" is a button that calls this (e.g. to open the
   * ViewRolloutModal in place) instead of navigating. Takes precedence over
   * `detailsPath`. */
  onViewRollout?: () => void;
}

function pillDotClass(event: RolloutEvent): string {
  switch (event.state) {
    case "completedWithFailures":
      return "bg-intent-critical-fill";
    case "completed":
      return "bg-intent-success-fill";
    case "paused":
    case "pausedAtPilotGate":
    case "pausedAtBatchReview":
      return "bg-core-accent-fill";
    default:
      return "bg-intent-warning-fill";
  }
}

function pillStatePhrase(event: RolloutEvent): string {
  switch (event.state) {
    case "inProgress":
      return event.processType === "curtailment" ? "active" : "in progress";
    case "stabilizingTelemetry":
      return "waiting for telemetry";
    case "pausedAtPilotGate":
      return "paused for pilot review";
    case "pausedAtBatchReview":
      return "paused for batch review";
    case "paused":
      return "paused";
    case "scheduled":
      return "scheduled";
    case "completed":
      return "completed";
    case "completedWithFailures":
      return "completed with failures";
  }
}

/**
 * The persistent header pill for an active rollout, always-on entry point from
 * any page, opening a popover with quick status and a link to the detail. Built
 * on the same {@link PageHeaderPopoverPill} the CurtailmentPill uses.
 */
function RolloutPill({ event, detailsPath, onViewRollout }: RolloutPillProps): ReactElement {
  const done = rolloutPhaseCount(event.rollups, "done");
  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const batchDetail =
    event.currentBatch && event.totalBatches ? `Batch ${event.currentBatch} of ${event.totalBatches}` : null;

  const detailRows = [
    { id: "scope", value: event.scopeLabel },
    { id: "pacing", value: pacingSummary(event) },
    {
      id: "progress",
      value: `${done.toLocaleString()} of ${inScope.toLocaleString()} ${phaseLabel(event.processType, "done").toLowerCase()}`,
    },
    ...(batchDetail ? [{ id: "batch", value: batchDetail }] : []),
  ];

  const viewLabel = `View ${rolloutActionNoun(event.processType)}`;

  return (
    <PageHeaderPopoverPill
      ariaLabel={`${viewLabel} details for ${event.title}`}
      dotClassName={pillDotClass(event)}
      triggerClassName="rollout-pill-trigger"
      triggerLabel={`${rolloutProcessLabel(event.processType)} ${pillStatePhrase(event)}`}
    >
      {({ closePopover }) => (
        <div className="flex flex-col gap-3">
          <div className="min-w-0 space-y-1">
            <div className="truncate text-heading-100 text-text-primary">{event.title}</div>
            {detailRows.map(({ id, value }) => (
              <div key={id} className="text-200 leading-snug text-text-primary-70">
                {value}
              </div>
            ))}
          </div>

          {onViewRollout ? (
            <div className="border-t border-border-5 pt-3">
              <button
                type="button"
                onClick={() => {
                  closePopover();
                  onViewRollout();
                }}
                className="block w-full rounded-xl px-3 py-2.5 text-left text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
              >
                {viewLabel}
              </button>
            </div>
          ) : detailsPath ? (
            <div className="border-t border-border-5 pt-3">
              <Link
                to={detailsPath}
                onClick={closePopover}
                className="block rounded-xl px-3 py-2.5 text-emphasis-300 text-text-primary transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5"
              >
                {viewLabel}
              </Link>
            </div>
          ) : null}
        </div>
      )}
    </PageHeaderPopoverPill>
  );
}

export default RolloutPill;
