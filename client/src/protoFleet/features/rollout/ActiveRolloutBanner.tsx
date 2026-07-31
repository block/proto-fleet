import type { ReactElement } from "react";

import { phaseLabel, rolloutPhaseCount } from "./rolloutDisplayUtils";
import type { RolloutEvent, RolloutProcessType } from "./rolloutTypes";
import { Download, LightningAlt, Reboot } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";

interface ActiveRolloutBannerProps {
  event: RolloutEvent;
  onView?: () => void;
}

interface ActiveRolloutBannerStackProps {
  events: RolloutEvent[];
  onView?: (event: RolloutEvent, index: number) => void;
}

/** Intent per process — firmware/curtailment carry uptime impact (warning),
 * reboot is informational. Drives the shared Callout's color + icon tint. */
const processIntent: Record<RolloutProcessType, keyof typeof intents> = {
  firmware: intents.warning,
  curtailment: intents.warning,
  reboot: intents.information,
};

function ProcessIcon({ processType }: { processType: RolloutProcessType }): ReactElement {
  // Force neutral/black icons regardless of the Callout's intent tint — the
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
  const done = rolloutPhaseCount(event.rollups, "done");
  const failed = rolloutPhaseCount(event.rollups, "failed");
  const inScope = Math.max(event.totalTargets - event.excludedTargets, 0);
  const doneVerb = phaseLabel(event.processType, "done").toLowerCase();

  const parts = [`${done.toLocaleString()} of ${inScope.toLocaleString()} miners ${doneVerb}`];
  if (failed > 0) {
    parts.push(`${failed.toLocaleString()} failed`);
  }
  if (event.currentBatch && event.totalBatches) {
    parts.push(`Batch ${event.currentBatch} of ${event.totalBatches}`);
  }
  return parts.join(", ");
}

/**
 * Inline progress banner for an active rollout, built on the shared {@link Callout}
 * — our standard inline banner — so a rollout reads exactly like every other
 * page banner. Stacks via {@link ActiveRolloutBannerStack} when several
 * processes run at once (the Activity "Active now" surface).
 */
export function ActiveRolloutBanner({ event, onView }: ActiveRolloutBannerProps): ReactElement {
  return (
    <Callout
      intent={processIntent[event.processType]}
      prefixIcon={<ProcessIcon processType={event.processType} />}
      title={bannerTitle(event)}
      subtitle={bannerSubtitle(event)}
      buttonText={onView ? "View rollout" : undefined}
      buttonOnClick={onView}
      testId="active-rollout-banner"
    />
  );
}

export function ActiveRolloutBannerStack({ events, onView }: ActiveRolloutBannerStackProps): ReactElement {
  return (
    <div className="flex flex-col gap-3" data-testid="active-rollout-banner-stack">
      {events.map((event, index) => (
        <ActiveRolloutBanner
          key={`${event.processType}-${event.title}-${index}`}
          event={event}
          onView={onView ? () => onView(event, index) : undefined}
        />
      ))}
    </div>
  );
}

export default ActiveRolloutBanner;
