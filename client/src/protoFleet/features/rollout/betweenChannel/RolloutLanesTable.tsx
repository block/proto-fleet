import {
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
          className="grid gap-0.5 text-left text-200 text-text-primary-70 underline-offset-2 hover:underline"
          aria-label={`View initial firmware setup for ${lane.label}`}
          onClick={() => onSetup(lane)}
        >
          <span>
            {lane.initialEnforcement.pendingCount.toLocaleString()} {firmwareTransitionDisplay.pending.countLabel}
          </span>
          <span>
            {lane.initialEnforcement.updatingCount.toLocaleString()} {firmwareTransitionDisplay.updating.countLabel}
          </span>
          <span>
            {lane.initialEnforcement.verifyingCount.toLocaleString()} {firmwareTransitionDisplay.verifying.countLabel}
          </span>
          <span>
            {lane.initialEnforcement.confirmedCount.toLocaleString()} {firmwareTransitionDisplay.confirmed.countLabel}
          </span>
          <span>
            {lane.initialEnforcement.attentionCount.toLocaleString()}{" "}
            {firmwareTransitionDisplay.needsAttention.countLabel}
          </span>
        </button>
      ),
      width: "w-36",
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
            {canStart && startBlockedReason ? (
              <div className="max-w-64 text-right text-200 text-text-primary-70">{startBlockedReason}</div>
            ) : null}
            {canDelete && deleteBlockedReason ? (
              <div className="max-w-64 text-right text-200 text-text-primary-70">{deleteBlockedReason}</div>
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
