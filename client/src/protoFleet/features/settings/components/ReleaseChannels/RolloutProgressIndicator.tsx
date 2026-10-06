import { useId, useRef, useState } from "react";

import {
  isPaused,
  modelUpdateStatus,
  pairLabel,
  rolloutDeviceCounts,
  rolloutProgressSummary,
  scopedToBatch,
} from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ChannelModelGroupView } from "@/protoFleet/api/useReleaseChannels";
import { useClickOutsideDismiss } from "@/shared/hooks/useClickOutsideDismiss";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

interface RolloutProgressIndicatorProps {
  group: ChannelModelGroupView;
  rollout: Rollout;
  testId?: string;
}

const RolloutProgressIndicator = ({ group, rollout, testId }: RolloutProgressIndicatorProps) => {
  const tooltipId = useId();
  const containerRef = useRef<HTMLSpanElement>(null);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [pinned, setPinned] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const showTooltip = !dismissed && (hovered || focused || pinned);
  const counts = rolloutDeviceCounts(rollout);
  const status = modelUpdateStatus(group, rollout, undefined);
  const target = `Updating to ${rollout.firmwareVersion}`;
  const label = `${pairLabel(group)}: ${status.label}. ${target}`;
  const summary = rolloutProgressSummary(counts);
  const omitted = [
    counts.skipped > 0 ? `${counts.skipped.toLocaleString()} skipped` : "",
    counts.excluded > 0 ? `${counts.excluded.toLocaleString()} excluded` : "",
  ]
    .filter(Boolean)
    .join(", ");
  const scope = scopedToBatch(rollout) ? "Current batch" : "Status";
  const attention = status.tone === "attention" || counts.failed > 0;

  const dismissTooltip = () => {
    setDismissed(true);
    setPinned(false);
  };
  useEscapeDismiss(showTooltip ? dismissTooltip : undefined);
  useClickOutsideDismiss({ ref: containerRef, onDismiss: showTooltip ? dismissTooltip : undefined });

  return (
    <span
      ref={containerRef}
      className="relative inline-flex h-8 w-full max-w-48 min-w-0 items-center align-middle"
      onMouseEnter={() => {
        setHovered(true);
        setDismissed(false);
      }}
      onMouseLeave={() => setHovered(false)}
    >
      <span
        className="flex w-full min-w-0 items-center gap-2"
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={counts.percent}
        aria-valuetext={`${scope}: ${status.label}. Overall: ${summary}${omitted ? `. ${omitted}` : ""}`}
        data-testid={testId}
      >
        <span aria-hidden="true" className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-core-primary-10">
          <span className="block h-full rounded-full bg-core-primary-fill" style={{ width: `${counts.percent}%` }} />
        </span>
        {attention ? (
          <span
            aria-hidden="true"
            className="flex size-4 shrink-0 items-center justify-center text-emphasis-200 text-intent-critical-fill"
            data-testid="rollout-attention-indicator"
          >
            !
          </span>
        ) : isPaused(rollout) ? (
          <span
            aria-hidden="true"
            className="flex size-4 shrink-0 items-center justify-center gap-0.5"
            data-testid="rollout-paused-indicator"
          >
            <span className="h-2.5 w-0.5 rounded-full bg-core-primary-fill" />
            <span className="h-2.5 w-0.5 rounded-full bg-core-primary-fill" />
          </span>
        ) : null}
      </span>
      <button
        type="button"
        aria-label={`Show update details for ${pairLabel(group)}`}
        aria-describedby={showTooltip ? tooltipId : undefined}
        className="absolute inset-0 cursor-help rounded focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base focus-visible:outline-none"
        onFocus={() => {
          setFocused(true);
          setDismissed(false);
        }}
        onBlur={() => {
          setFocused(false);
          setPinned(false);
        }}
        onClick={() => {
          setPinned(!pinned);
          setDismissed(pinned);
        }}
      />
      {showTooltip ? (
        <span className="absolute right-0 bottom-full z-50 w-72 max-w-[calc(100vw-2rem)] pb-2">
          <span
            id={tooltipId}
            role="tooltip"
            className="block rounded-lg bg-surface-base p-3 text-200 text-text-primary shadow-200"
          >
            <span className="block text-emphasis-200">{target}</span>
            <span className="mt-1 block">
              {scope}: {status.label}
            </span>
            <span className="mt-1 block text-text-primary-70">Overall: {summary}</span>
            {omitted ? (
              <span className="mt-1 block text-text-primary-70">{omitted} (not included in progress)</span>
            ) : null}
          </span>
        </span>
      ) : null}
    </span>
  );
};

export default RolloutProgressIndicator;
