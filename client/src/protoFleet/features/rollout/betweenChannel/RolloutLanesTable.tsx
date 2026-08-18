import {
  dominantInitialFirmwareState,
  rolloutLaneActionStatus,
  rolloutLaneDeleteBlockedReason,
  rolloutLaneStartBlockedReason,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import { firmwareTransitionDisplay } from "@/protoFleet/features/rollout/firmwareTransitionDisplay";
import type { RolloutLane, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import Button, { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";

export interface LaneTableRow {
  id: string;
  lane: RolloutLane;
  latestRollout?: RolloutRecord;
}

interface RolloutLanesTableProps {
  rows: LaneTableRow[];
  canStart: boolean;
  canDelete?: boolean;
  deletePermissionBlockedReason?: string;
  isPreparingStart?: boolean;
  onSetup: (lane: RolloutLane) => void;
  onStart: (lane: RolloutLane) => void;
  onView: (rollout: RolloutRecord) => void;
  onDelete?: (lane: RolloutLane) => void;
}

type LaneColumn = "lane" | "release" | "members" | "initial" | "rollout" | "actions";

const columns: LaneColumn[] = ["lane", "release", "members", "initial", "rollout", "actions"];
const titles: ColTitles<LaneColumn> = {
  lane: "Rollout lane",
  release: "Current release",
  members: "Miners",
  initial: "Initial firmware",
  rollout: "Latest rollout",
  actions: "",
};

function releaseSummary(lane: RolloutLane): string {
  if (lane.currentReleaseTargets.length === 0) {
    return "Release unavailable";
  }
  return lane.currentReleaseTargets.map((target) => `${target.targetModel} ${target.firmwareVersion}`).join(", ");
}

function rolloutStateLabel(state: RolloutRecord["state"]): string {
  switch (state) {
    case "completedWithFailures":
      return "Completed with failures";
    default:
      return state.replace(/([A-Z])/g, " $1").replace(/^./, (value) => value.toUpperCase());
  }
}

function initialFirmwareSummary(lane: RolloutLane): string {
  const { totalCount, confirmedCount, attentionCount } = lane.initialEnforcement;
  const dominantState = dominantInitialFirmwareState(lane);
  if (dominantState === "needsAttention") {
    const attentionLabel = attentionCount === 1 ? "needs attention" : "need attention";
    return `${confirmedCount.toLocaleString()}/${totalCount.toLocaleString()} confirmed · ${attentionCount.toLocaleString()} ${attentionLabel}`;
  }
  return dominantState === "confirmed"
    ? `${confirmedCount.toLocaleString()} confirmed`
    : `${confirmedCount.toLocaleString()}/${totalCount.toLocaleString()} confirmed · ${firmwareTransitionDisplay[dominantState].summaryLabel}`;
}

export default function RolloutLanesTable({
  rows,
  canStart,
  canDelete = false,
  deletePermissionBlockedReason,
  isPreparingStart = false,
  onSetup,
  onStart,
  onView,
  onDelete,
}: RolloutLanesTableProps) {
  const colConfig: ColConfig<LaneTableRow, string, LaneColumn> = {
    lane: {
      component: ({ lane }) => (
        <div>
          <div className="text-emphasis-300 text-text-primary">{lane.label}</div>
          {lane.description ? <div className="mt-1 text-200 text-text-primary-70">{lane.description}</div> : null}
        </div>
      ),
      width: "w-64",
      allowWrap: true,
    },
    release: {
      component: ({ lane }) => <span>{releaseSummary(lane)}</span>,
      width: "w-72",
      allowWrap: true,
    },
    members: {
      component: ({ lane }) => lane.memberCount.toLocaleString(),
      width: "w-28",
    },
    initial: {
      component: ({ lane }) => (
        <button
          type="button"
          className="text-left text-200 whitespace-normal text-text-primary-70 underline-offset-2 hover:underline"
          aria-label={`View initial firmware setup for ${lane.label}`}
          onClick={() => onSetup(lane)}
        >
          <span>{initialFirmwareSummary(lane)}</span>
        </button>
      ),
      width: "w-36",
      allowWrap: true,
    },
    rollout: {
      component: ({ latestRollout }) =>
        latestRollout ? (
          <button
            type="button"
            className="text-left text-300 text-text-primary underline underline-offset-2"
            onClick={() => onView(latestRollout)}
          >
            {latestRollout.name}: {rolloutStateLabel(latestRollout.state)}
          </button>
        ) : (
          <span className="text-text-primary-50">No rollouts yet</span>
        ),
      width: "w-72",
      allowWrap: true,
    },
    actions: {
      component: ({ lane, latestRollout }) => {
        const startBlockedReason = rolloutLaneStartBlockedReason(lane, latestRollout);
        const deleteBlockedReason =
          deletePermissionBlockedReason ?? rolloutLaneDeleteBlockedReason(lane, latestRollout);
        const actionStatus = rolloutLaneActionStatus(lane, latestRollout, {
          canStart,
          canDelete: canDelete && onDelete !== undefined,
          deletePermissionBlockedReason,
        });
        return (
          <div className="flex flex-col items-end gap-2">
            <div className="flex justify-end gap-2">
              {latestRollout ? (
                <Button
                  text="View"
                  ariaLabel={`View latest rollout for ${lane.label}`}
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => onView(latestRollout)}
                />
              ) : null}
              {canStart ? (
                <Button
                  text="Start rollout"
                  ariaLabel={`Start rollout for ${lane.label}`}
                  variant={variants.primary}
                  size={sizes.compact}
                  disabled={isPreparingStart || startBlockedReason !== null}
                  onClick={() => onStart(lane)}
                />
              ) : null}
              {canDelete && onDelete ? (
                <Button
                  text="Delete"
                  ariaLabel={`Delete ${lane.label}`}
                  variant={variants.danger}
                  size={sizes.compact}
                  disabled={deleteBlockedReason !== null}
                  onClick={() => onDelete(lane)}
                />
              ) : null}
            </div>
            {actionStatus ? (
              <div className="max-w-64 text-right text-200 text-text-primary-70">{actionStatus}</div>
            ) : null}
          </div>
        );
      },
      width: "w-64",
    },
  };

  return (
    <List<LaneTableRow, string, LaneColumn>
      items={rows}
      itemKey="id"
      activeCols={columns}
      colTitles={titles}
      colConfig={colConfig}
      total={rows.length}
      itemName={{ singular: "rollout lane", plural: "rollout lanes" }}
      applyColumnWidthsToCells
      stickyFirstColumn={false}
      noDataElement={
        <SettingsEmptyState
          title="No rollout lanes"
          description="Create a stable lane to manage firmware releases without exposing version-channel churn."
        />
      }
    />
  );
}
