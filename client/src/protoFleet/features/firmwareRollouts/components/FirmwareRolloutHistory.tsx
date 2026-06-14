import type { ReactElement } from "react";
import clsx from "clsx";

import { type FirmwareRollout } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import FirmwareRolloutStatusPill from "@/protoFleet/features/firmwareRollouts/components/FirmwareRolloutStatusPill";
import {
  formatRolloutTimestamp,
  getRolloutProgress,
} from "@/protoFleet/features/firmwareRollouts/firmwareRolloutDisplayUtils";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface FirmwareRolloutHistoryProps {
  rollouts: FirmwareRollout[];
  className?: string;
  onSelect: (rollout: FirmwareRollout) => void;
}

const historyColumns = ["Rollout", "Model", "Results", "Status", "Finished"] as const;

const FirmwareRolloutHistory = ({ rollouts, className, onSelect }: FirmwareRolloutHistoryProps): ReactElement => {
  return (
    <section className={clsx("grid gap-4", className)}>
      <Header title="Rollout history" titleSize="text-heading-200" />

      {rollouts.length === 0 ? (
        <div className="rounded-xl border border-border-5 bg-surface-base p-6 text-300 text-text-primary-50">
          No completed rollouts yet.
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] table-fixed text-left text-300">
            <thead>
              <tr className="border-b border-border-5 text-text-primary-50">
                {historyColumns.map((column) => (
                  <th key={column} className="py-3 pr-6 font-normal">
                    <span className="text-emphasis-300 text-text-primary-50">{column}</span>
                  </th>
                ))}
                <th className="w-28 py-3 font-normal" aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {rollouts.map((rollout) => {
                const progress = getRolloutProgress(rollout);
                return (
                  <tr
                    key={rollout.rolloutId}
                    className="cursor-pointer border-b border-border-5 last:border-0 hover:bg-surface-5"
                    onClick={() => onSelect(rollout)}
                  >
                    <td className="py-4 pr-6 align-top">
                      <div className="truncate text-emphasis-300 text-text-primary" title={rollout.name}>
                        {rollout.name}
                      </div>
                      <div className="text-200 text-text-primary-50">
                        Started {formatRolloutTimestamp(rollout.startedAt?.seconds)}
                      </div>
                    </td>
                    <td className="py-4 pr-6 align-top text-text-primary">{rollout.minerModel || "—"}</td>
                    <td className="py-4 pr-6 align-top">
                      <div className="text-text-primary">
                        {progress.success.toLocaleString()} / {progress.total.toLocaleString()} succeeded
                      </div>
                      {progress.failure > 0 ? (
                        <div className="text-200 text-text-critical">{progress.failure.toLocaleString()} failed</div>
                      ) : null}
                    </td>
                    <td className="py-4 pr-6 align-top">
                      <FirmwareRolloutStatusPill state={rollout.state} />
                    </td>
                    <td className="py-4 pr-6 align-top text-text-primary-50">
                      {formatRolloutTimestamp(rollout.endedAt?.seconds)}
                    </td>
                    <td className="py-4 text-right align-top">
                      <Button
                        variant={variants.secondary}
                        size={sizes.compact}
                        text="View"
                        onClick={(event) => {
                          event.stopPropagation();
                          onSelect(rollout);
                        }}
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
};

export default FirmwareRolloutHistory;
