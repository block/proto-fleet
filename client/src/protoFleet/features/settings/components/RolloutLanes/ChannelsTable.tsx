import { useState } from "react";
import clsx from "clsx";

import { ModelStatusCell, StatusCell } from "./channelStatus";
import { isAwaitingReview, modelFirmwareLabel } from "./rolloutStatus";
import { type Rollout, type RolloutLane, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";

interface ChannelsTableProps {
  lanes: RolloutLane[];
  rollouts: Rollout[];
  onManage: (lane: RolloutLane) => void;
}

// The release channels overview: one expandable row per channel with
// aggregate counts, and per-model sub-rows carrying firmware targets and
// update state, mirroring the firmware release channels design.
const ChannelsTable = ({ lanes, rollouts, onManage }: ChannelsTableProps) => {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const activeByLaneModel = new Map(
    rollouts
      .filter((rollout) => rollout.status === RolloutStatus.ACTIVE)
      .map((rollout) => [`${rollout.laneId.toString()}:${rollout.model}`, rollout]),
  );

  return (
    <table className="w-full text-left text-200" data-testid="channels-table">
      <thead>
        <tr className="text-text-primary-50">
          <th className="py-1.5 pr-4 font-normal">Release channel / model</th>
          <th className="py-1.5 pr-4 font-normal">Miners</th>
          <th className="py-1.5 pr-4 font-normal">Firmware</th>
          <th className="py-1.5 pr-4 font-normal">Update status</th>
          <th className="py-1.5 font-normal">
            <span className="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      {lanes.map((lane) => {
        const laneKey = lane.id.toString();
        const isExpanded = expanded[laneKey] === true;
        const memberCount = lane.modelGroups.reduce((sum, group) => sum + group.miners.length, 0);
        const laneActive = lane.modelGroups
          .map((group) => activeByLaneModel.get(`${laneKey}:${group.model}`))
          .filter((rollout): rollout is Rollout => rollout !== undefined);
        const activeCount = laneActive.length;
        const reviewCount = laneActive.filter(isAwaitingReview).length;

        return (
          <tbody key={laneKey} className="text-text-primary">
            <tr className="border-t border-border-5" data-testid={`channel-row-${lane.name}`}>
              <td className="py-3 pr-4">
                <button
                  type="button"
                  aria-expanded={isExpanded}
                  data-testid={`channel-toggle-${lane.name}`}
                  className="flex cursor-pointer items-center gap-2 text-left text-emphasis-300"
                  onClick={() => setExpanded((current) => ({ ...current, [laneKey]: !isExpanded }))}
                >
                  <ChevronDown
                    width="w-3"
                    className={clsx("shrink-0 transition-transform", !isExpanded && "-rotate-90")}
                  />
                  {lane.name}
                </button>
              </td>
              <td className="py-3 pr-4" data-testid={`channel-miners-${lane.name}`}>
                {memberCount.toLocaleString()}
              </td>
              <td className="py-3 pr-4">
                {lane.modelGroups.length === 1 ? "1 model" : `${lane.modelGroups.length} models`}
              </td>
              <td className="py-3 pr-4" data-testid={`channel-status-${lane.name}`}>
                {reviewCount > 0 ? (
                  <StatusCell dotClassName="bg-intent-warning-fill" label={`${activeCount} active · review needed`} />
                ) : activeCount > 0 ? (
                  <StatusCell dotClassName="animate-pulse bg-intent-warning-fill" label={`${activeCount} active`} />
                ) : (
                  <StatusCell dotClassName="bg-core-primary-10" label="No active updates" />
                )}
              </td>
              <td className="py-3 text-right">
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  text="Manage"
                  onClick={() => onManage(lane)}
                  testId={`manage-channel-${lane.name}`}
                />
              </td>
            </tr>
            {isExpanded
              ? lane.modelGroups.map((group) => (
                  <tr
                    key={group.model}
                    className="border-t border-border-5 text-text-primary-70"
                    data-testid={`model-row-${lane.name}-${group.model}`}
                  >
                    <td className="py-2.5 pr-4 pl-8">{group.model || "Unknown model"}</td>
                    <td className="py-2.5 pr-4">{group.miners.length.toLocaleString()}</td>
                    <td className="py-2.5 pr-4">{modelFirmwareLabel(group)}</td>
                    <td className="py-2.5 pr-4">
                      <ModelStatusCell
                        group={group}
                        activeRollout={activeByLaneModel.get(`${laneKey}:${group.model}`)}
                      />
                    </td>
                    <td className="py-2.5" />
                  </tr>
                ))
              : null}
            {isExpanded && lane.modelGroups.length === 0 ? (
              <tr className="border-t border-border-5 text-text-primary-50">
                <td className="py-2.5 pr-4 pl-8" colSpan={5}>
                  Empty channel. Use Manage to add miners.
                </td>
              </tr>
            ) : null}
          </tbody>
        );
      })}
    </table>
  );
};

export default ChannelsTable;
