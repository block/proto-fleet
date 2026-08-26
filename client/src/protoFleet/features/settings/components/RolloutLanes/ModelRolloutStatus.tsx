import { type ReactElement, useEffect, useState } from "react";
import clsx from "clsx";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import {
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
} from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { formatCurtailmentElapsedDuration as formatElapsed } from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import CompositionBar from "@/shared/components/CompositionBar";
import ProgressCircular from "@/shared/components/ProgressCircular";

const millisecondsPerSecond = 1000;

// Ticks once per second so the elapsed readout moves between polling
// snapshots. Lives in its own component so the per-second tick re-renders
// only this value, not the whole card (same pattern as the curtailment card).
function ElapsedProgressValue({ sinceMs }: { sinceMs: number }): ReactElement {
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    const intervalId = setInterval(() => setNowMs(Date.now()), millisecondsPerSecond);
    return () => clearInterval(intervalId);
  }, []);

  const elapsedSeconds = Math.max((nowMs - sinceMs) / millisecondsPerSecond, 0);
  return <div className="text-right text-200 text-text-primary">{`${formatElapsed(elapsedSeconds)} elapsed`}</div>;
}

// Live progress card for an ongoing per-model rollout, following the active
// curtailment status card: status icon, primary lockup with lane and model
// context, progress summary with a live elapsed timer, segmented composition
// bar, and legend.
function ModelRolloutStatus({ rollout }: { rollout: Rollout }): ReactElement {
  const counts = rolloutDeviceCounts(rollout);
  const segments = rolloutProgressSegments(counts);
  const startedAtMs = rollout.createdAt ? timestampMs(rollout.createdAt) : undefined;

  return (
    <div
      className="rounded-xl bg-surface-elevated-base p-6 shadow-100"
      data-testid={`model-rollout-status-${rollout.id.toString()}`}
    >
      <div className="flex items-center gap-3">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-core-primary-5">
          <ProgressCircular indeterminate />
        </div>
        <div className="min-w-0">
          <div className="text-heading-50 text-text-primary-70">Firmware rollout</div>
          <div className="truncate text-heading-200 text-text-primary">{`Rolling out ${rollout.firmwareVersion}`}</div>
          <div className="mt-0.5 truncate text-200 text-text-primary-70">
            {`${rollout.laneName} · ${rollout.model}`}
          </div>
        </div>
      </div>

      <div className="mt-4 grid gap-3" data-testid={`model-rollout-progress-${rollout.id.toString()}`}>
        <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1">
          <div className="text-200 text-text-primary-50">{rolloutProgressSummary(counts)}</div>
          {startedAtMs !== undefined ? <ElapsedProgressValue sinceMs={startedAtMs} /> : null}
        </div>
        <CompositionBar segments={segments} height={12} colorMap={rolloutProgressColorMap} />
        <div className="flex flex-wrap items-start gap-x-5 gap-y-1 text-200 text-text-primary-70">
          {segments.map((segment) => (
            <span key={segment.name} className="flex items-start gap-2">
              <span
                className={clsx(
                  "mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full",
                  rolloutProgressColorMap[segment.status],
                )}
              />
              {`${segment.name} (${(segment.count ?? 0).toLocaleString()})`}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

export default ModelRolloutStatus;
