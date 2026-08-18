import { type ReactElement, useState } from "react";
import clsx from "clsx";

import { firmwareTransitionDisplay } from "./firmwareTransitionDisplay";
import type { FirmwareTransitionProgress, FirmwareTransitionState } from "./rolloutTypes";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle from "@/shared/components/StatusCircle";

interface FirmwareTransitionMinerDetailsProps {
  progress: FirmwareTransitionProgress;
  className?: string;
}

const minerPageSize = 100;

function TransitionState({ state }: { state: FirmwareTransitionState }): ReactElement {
  const active = state === "updating" || state === "verifying";
  const display = firmwareTransitionDisplay[state];
  return (
    <span className="flex items-center gap-2 whitespace-nowrap">
      <StatusCircle status={display.status} variant="simple" width="w-[6px]" />
      {active ? <ProgressCircular size={14} indeterminate /> : null}
      <span>{display.tableLabel}</span>
    </span>
  );
}

export default function FirmwareTransitionMinerDetails({
  progress,
  className,
}: FirmwareTransitionMinerDetailsProps): ReactElement {
  const [visibleMinerCount, setVisibleMinerCount] = useState(minerPageSize);
  const visibleMembers = progress.members.slice(0, visibleMinerCount);

  if (progress.members.length === 0) {
    return (
      <div
        className={clsx(
          "rounded-xl border border-border-5 bg-surface-base px-4 py-8 text-center text-300 text-text-primary-70",
          className,
        )}
      >
        Miner details are unavailable.
      </div>
    );
  }

  return (
    <div className={clsx("overflow-x-auto rounded-xl border border-border-5 bg-surface-base", className)}>
      <table className="w-full min-w-[960px] text-left text-300">
        <thead className="border-b border-border-5 text-200 text-text-primary-50">
          <tr>
            <th className="px-4 py-3 font-normal">Miner</th>
            <th className="px-4 py-3 font-normal">Manufacturer and model</th>
            <th className="px-4 py-3 font-normal">Latest observed firmware</th>
            <th className="px-4 py-3 font-normal">Target firmware</th>
            <th className="px-4 py-3 font-normal">State</th>
            <th className="px-4 py-3 font-normal">Details</th>
          </tr>
        </thead>
        <tbody>
          {visibleMembers.map((miner) => (
            <tr key={miner.deviceIdentifier} className="border-b border-border-5 last:border-b-0">
              <td className="px-4 py-3 text-emphasis-300 text-text-primary">{miner.deviceIdentifier}</td>
              <td className="px-4 py-3 text-text-primary">
                {[miner.manufacturer, miner.model].filter(Boolean).join(" ") || "Unknown"}
              </td>
              <td className="px-4 py-3 text-text-primary">
                {miner.latestObservedFirmwareVersion?.trim() || "Unknown"}
              </td>
              <td className="px-4 py-3 text-text-primary">{miner.targetFirmwareVersion}</td>
              <td className="px-4 py-3 text-text-primary">
                <TransitionState state={miner.state} />
              </td>
              <td className="max-w-80 px-4 py-3 text-text-primary-70">{miner.lastError ?? "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {visibleMembers.length < progress.members.length ? (
        <div className="flex items-center justify-between gap-4 border-t border-border-5 px-4 py-3 text-200">
          <span className="text-text-primary-70">
            Showing {visibleMembers.length.toLocaleString()} of {progress.members.length.toLocaleString()} miners
          </span>
          <button
            type="button"
            className="text-text-primary underline underline-offset-2"
            onClick={() => setVisibleMinerCount((current) => current + minerPageSize)}
          >
            Show more miners
          </button>
        </div>
      ) : null}
    </div>
  );
}
