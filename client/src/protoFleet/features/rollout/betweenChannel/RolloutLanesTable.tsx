import {
  dominantFirmwareConvergenceState,
  rolloutLaneActionStatus,
  rolloutLaneDeleteBlockedReason,
  rolloutLaneStartBlockedReason,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import { firmwareTransitionDisplay } from "@/protoFleet/features/rollout/firmwareTransitionDisplay";
import type { RolloutLane, RolloutLaneModel, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
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
  onManageMembers?: (lane: RolloutLane) => void;
  onManageDeclarations?: (lane: RolloutLane) => void;
  onStart: (lane: RolloutLane) => void;
  onView: (rollout: RolloutRecord) => void;
  onDelete?: (lane: RolloutLane) => void;
}

type LaneColumn = "lane" | "release" | "members" | "firmware" | "rollout" | "actions";

const columns: LaneColumn[] = ["lane", "release", "members", "firmware", "rollout", "actions"];
const titles: ColTitles<LaneColumn> = {
  lane: "Rollout lane",
  release: "Current release",
  members: "Miners",
  firmware: "Firmware status",
  rollout: "Latest rollout",
  actions: "",
};

function releaseSummary(lane: RolloutLane): string {
  if (lane.currentReleaseTargets.length === 0) {
    return "Release unavailable";
  }
  return lane.currentReleaseTargets.map((target) => `${target.targetModel} ${target.firmwareVersion}`).join(", ");
}

function modelConvergenceSummary(model: RolloutLaneModel): string {
  if (model.memberCount === 0) {
    return "No members";
  }
  const { totalCount, confirmedCount, attentionCount, verifyingCount, updatingCount, pendingCount } =
    model.firmwareConvergence;
  if (attentionCount > 0) {
    return `${confirmedCount.toLocaleString()}/${totalCount.toLocaleString()} confirmed · ${attentionCount.toLocaleString()} need attention`;
  }
  if (verifyingCount > 0) {
    return `${confirmedCount.toLocaleString()}/${totalCount.toLocaleString()} confirmed · Verifying`;
  }
  if (updatingCount > 0) {
    return `${confirmedCount.toLocaleString()}/${totalCount.toLocaleString()} confirmed · Updating`;
  }
  if (pendingCount > 0) {
    return `${confirmedCount.toLocaleString()}/${totalCount.toLocaleString()} confirmed · Pending`;
  }
  return `${confirmedCount.toLocaleString()} confirmed`;
}

function modelDeclarations(lane: RolloutLane) {
  if (!lane.models.length) {
    return <span>{releaseSummary(lane)}</span>;
  }
  return (
    <div className="grid gap-3">
      {lane.models.map((model) => {
        const target = model.compatibility === "compatible" ? model.currentFirmwareTarget : undefined;
        return (
          <div
            key={model.id}
            role="group"
            aria-label={`${model.manufacturer} ${model.model} model declaration`}
            className="rounded-lg border border-border-5 p-3"
          >
            <div className="text-emphasis-300 text-text-primary">
              {model.manufacturer} {model.model}
            </div>
            <div className="mt-1 grid gap-0.5 text-200 text-text-primary-70">
              <span>{target ? `Firmware ${target.firmwareVersion}` : "No compatible firmware"}</span>
              <span>
                {model.memberCount.toLocaleString()} {model.memberCount === 1 ? "miner" : "miners"}
              </span>
              <span>{target ? "Compatible" : "Target unavailable"}</span>
              <span>{modelConvergenceSummary(model)}</span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function displayedLaneMemberCount(lane: RolloutLane): number {
  return lane.models.length ? lane.models.reduce((total, model) => total + model.memberCount, 0) : lane.memberCount;
}

function rolloutStateLabel(state: RolloutRecord["state"]): string {
  switch (state) {
    case "completedWithFailures":
      return "Completed with failures";
    default:
      return state.replace(/([A-Z])/g, " $1").replace(/^./, (value) => value.toUpperCase());
  }
}

function firmwareConvergenceSummary(lane: RolloutLane): string {
  const { totalCount, confirmedCount, attentionCount } = lane.firmwareConvergence;
  if (lane.memberCount === 0) {
    return "No miners";
  }
  const dominantState = dominantFirmwareConvergenceState(lane);
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
  onManageMembers,
  onManageDeclarations,
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
          {lane.topologyEnabled && lane.scalarProjectionAvailable === false ? (
            <div className="mt-1 text-200 text-text-primary-70">Models use different physical channels</div>
          ) : null}
        </div>
      ),
      width: "w-64",
      allowWrap: true,
    },
    release: {
      component: ({ lane }) => modelDeclarations(lane),
      width: "w-72",
      allowWrap: true,
    },
    members: {
      component: ({ lane }) => {
        const memberCount = displayedLaneMemberCount(lane);
        return onManageMembers ? (
          <button
            type="button"
            className="text-left text-300 text-text-primary underline underline-offset-2"
            aria-label={`Manage members for ${lane.label}`}
            onClick={() => onManageMembers(lane)}
          >
            {memberCount.toLocaleString()} {memberCount === 1 ? "miner" : "miners"}
          </button>
        ) : (
          <span>
            {memberCount.toLocaleString()} {memberCount === 1 ? "miner" : "miners"}
          </span>
        );
      },
      width: "w-28",
    },
    firmware: {
      component: ({ lane }) =>
        lane.models.length ? (
          <span className="text-200 text-text-primary-70">Shown by model</span>
        ) : (
          <button
            type="button"
            className="text-left text-200 whitespace-normal text-text-primary-70 underline-offset-2 hover:underline"
            aria-label={`View firmware status for ${lane.label}`}
            onClick={() => onSetup(lane)}
          >
            <span>{firmwareConvergenceSummary(lane)}</span>
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
              {onManageDeclarations && lane.topologyEnabled ? (
                <Button
                  text="Manage models"
                  ariaLabel={`Manage model declarations for ${lane.label}`}
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => onManageDeclarations(lane)}
                />
              ) : null}
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
