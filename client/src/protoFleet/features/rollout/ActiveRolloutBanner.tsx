import type { ReactElement } from "react";

import { phaseLabel, rolloutActionNoun, rolloutPhaseCount } from "./rolloutDisplayUtils";
import type { RolloutEvent, RolloutProcessType } from "./rolloutTypes";
import { Download, LightningAlt, Reboot } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

interface ActiveRolloutBannerProps {
  event: RolloutEvent;
  onView?: () => void;
  onManage?: () => void;
}

interface ActiveRolloutBannerStackProps {
  events: RolloutEvent[];
  onView?: (event: RolloutEvent, index: number) => void;
  onManage?: (event: RolloutEvent, index: number) => void;
}

/** Intent per process, firmware/curtailment carry uptime impact (warning),
 * reboot is informational. Drives the shared Callout's color + icon tint. */
const processIntent: Record<RolloutProcessType, keyof typeof intents> = {
  firmware: intents.warning,
  curtailment: intents.warning,
  reboot: intents.information,
};

function bannerIntent(event: RolloutEvent): keyof typeof intents {
  return event.state === "scheduled" ? intents.information : processIntent[event.processType];
}

function ProcessIcon({ processType }: { processType: RolloutProcessType }): ReactElement {
  // Force neutral/black icons regardless of the Callout's intent tint, the
  // intent color still drives the header/accent, but the process glyph stays
  // black for a calmer, more legible banner.
  const className = "text-text-primary";
  switch (processType) {
    case "firmware":
      return <Download className={className} />;
    case "reboot":
      return <Reboot className={className} />;
    case "curtailment":
      return <LightningAlt className={className} />;
  }
}

function bannerTitle(event: RolloutEvent): string {
  return event.scopeLabel ? `${event.title}, ${event.scopeLabel}` : event.title;
}

function bannerSubtitle(event: RolloutEvent): string {
  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);

  if (event.state === "scheduled") {
    const scheduledAt = event.scheduledStartAt ? formatTimestamp(isoToEpochSeconds(event.scheduledStartAt)) : undefined;
    const parts = [
      scheduledAt ? `Scheduled for ${scheduledAt}` : "Scheduled",
      `${inScope.toLocaleString()} miners queued`,
      event.excludedTargets > 0 ? `${event.excludedTargets.toLocaleString()} excluded` : null,
    ];
    return parts.filter(Boolean).join(", ");
  }

  const done = rolloutPhaseCount(event.rollups, "done");
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const doneVerb = phaseLabel(event.processType, "done").toLowerCase();

  const parts = [
    ...(event.state === "stabilizingTelemetry" ? ["Waiting for telemetry"] : []),
    `${done.toLocaleString()} of ${inScope.toLocaleString()} miners ${doneVerb}`,
  ];
  if (failed > 0) {
    parts.push(`${failed.toLocaleString()} failed`);
  }
  if (event.currentBatch && event.totalBatches) {
    parts.push(`Batch ${event.currentBatch} of ${event.totalBatches}`);
  }
  return parts.join(", ");
}

/**
 * Inline progress banner for active and scheduled rollouts.
 */
export function ActiveRolloutBanner({ event, onView, onManage }: ActiveRolloutBannerProps): ReactElement {
  const showManageAction = event.state === "scheduled" && onManage !== undefined;
  const showViewAction = event.state !== "scheduled" && onView !== undefined;
  const buttonText = showManageAction
    ? `Manage scheduled ${rolloutActionNoun(event.processType)}`
    : showViewAction
      ? `View ${rolloutActionNoun(event.processType)}`
      : undefined;
  const buttonOnClick = showManageAction ? onManage : showViewAction ? onView : undefined;

  return (
    <Callout
      intent={bannerIntent(event)}
      prefixIcon={<ProcessIcon processType={event.processType} />}
      title={bannerTitle(event)}
      subtitle={bannerSubtitle(event)}
      buttonText={buttonText}
      buttonOnClick={buttonOnClick}
      testId="active-rollout-banner"
    />
  );
}

export function ActiveRolloutBannerStack({ events, onView, onManage }: ActiveRolloutBannerStackProps): ReactElement {
  return (
    <div className="flex flex-col gap-3" data-testid="active-rollout-banner-stack">
      {events.map((event, index) => (
        <ActiveRolloutBanner
          key={`${event.processType}-${event.title}-${index}`}
          event={event}
          onView={onView ? () => onView(event, index) : undefined}
          onManage={onManage ? () => onManage(event, index) : undefined}
        />
      ))}
    </div>
  );
}

export default ActiveRolloutBanner;
