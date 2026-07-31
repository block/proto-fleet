import type { ReactElement } from "react";
import clsx from "clsx";

import { phaseLabel, rolloutPhaseCount } from "./rolloutDisplayUtils";
import type { RolloutEvent, RolloutProcessType } from "./rolloutTypes";
import { Download, LightningAlt, Reboot } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";

interface ActiveRolloutBannerProps {
  event: RolloutEvent;
  /** Highlights the banner as the currently-open detail (subtle ring). */
  selected?: boolean;
  onView?: () => void;
}

interface ActiveRolloutBannerStackProps {
  events: RolloutEvent[];
  selectedIndex?: number;
  onView?: (event: RolloutEvent, index: number) => void;
}

/**
 * Tone per process — firmware/curtailment read as "warning" (uptime-impacting),
 * reboot as "info". Matches the accent palette curtailment's banner uses.
 */
const processTone: Record<RolloutProcessType, "warning" | "info"> = {
  firmware: "warning",
  curtailment: "warning",
  reboot: "info",
};

function ProcessIcon({ processType }: { processType: RolloutProcessType }): ReactElement {
  const className = "text-current";
  switch (processType) {
    case "firmware":
      return <Download className={className} width="w-5" />;
    case "reboot":
      return <Reboot className={className} width="w-5" />;
    case "curtailment":
      return <LightningAlt className={className} width="w-5" />;
  }
}

function bannerSummary(event: RolloutEvent): string {
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
 * A single inline progress banner for an active rollout — the same shape as the
 * fleet/building in-progress banner, so a rollout reads identically wherever it
 * surfaces. Stacks via {@link ActiveRolloutBannerStack} when several processes
 * run at once.
 */
export function ActiveRolloutBanner({ event, selected = false, onView }: ActiveRolloutBannerProps): ReactElement {
  const tone = processTone[event.processType];
  return (
    <div
      className={clsx(
        "flex items-start gap-3 rounded-xl border p-4",
        tone === "warning"
          ? "border-core-accent-fill/20 bg-core-accent-fill/5 text-core-accent-fill"
          : "border-intent-info-fill/20 bg-intent-info-fill/5 text-intent-info-fill",
        selected && (tone === "warning" ? "ring-1 ring-core-accent-fill/40" : "ring-1 ring-intent-info-fill/40"),
      )}
      data-testid="active-rollout-banner"
    >
      <div className="mt-0.5 shrink-0">
        <ProcessIcon processType={event.processType} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-emphasis-300 text-text-primary">
          {event.title}
          {event.scopeLabel ? <span className="text-text-primary-70">{`, ${event.scopeLabel}`}</span> : null}
        </div>
        <div className="mt-1 text-300 text-text-primary-70">{bannerSummary(event)}</div>
      </div>
      {onView ? (
        <Button variant={variants.secondary} size={sizes.compact} text="View rollout" onClick={onView} />
      ) : null}
    </div>
  );
}

/**
 * The Activity "Active now" surface: active rollouts as a vertical stack of
 * banners rather than a list, one per running process.
 */
export function ActiveRolloutBannerStack({
  events,
  selectedIndex,
  onView,
}: ActiveRolloutBannerStackProps): ReactElement {
  return (
    <div className="flex flex-col gap-3" data-testid="active-rollout-banner-stack">
      {events.map((event, index) => (
        <ActiveRolloutBanner
          key={`${event.processType}-${event.title}-${index}`}
          event={event}
          selected={index === selectedIndex}
          onView={onView ? () => onView(event, index) : undefined}
        />
      ))}
    </div>
  );
}

export default ActiveRolloutBanner;
