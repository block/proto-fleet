import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import FirmwarePickerButton from "./FirmwarePickerButton";
import LaneHistoryModal from "./LaneHistoryModal";
import ModelMinersModal from "./ModelMinersModal";
import ModelRolloutStatus from "./ModelRolloutStatus";
import {
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
} from "./rolloutStatus";
import {
  Rollout,
  RolloutLane,
  RolloutLaneModelGroup,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { useRolloutLanes } from "@/protoFleet/api/useRolloutLanes";
import MinerSelectionModal from "@/protoFleet/components/TargetSelectionModal/MinerSelectionModal";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { Alert, ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import CompositionBar from "@/shared/components/CompositionBar";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const ROLLOUT_LANES_DESCRIPTION =
  "Group miners into channels and assign firmware per model. Assigned firmware is enforced: miners not on the assigned version are updated automatically.";

// Attention pill with a pulsing dot, shown wherever a rollout is ongoing.
const RolloutActivePill = ({ count, testId }: { count: number; testId?: string }) => (
  <span
    data-testid={testId}
    className="inline-flex items-center gap-1.5 rounded-full bg-intent-warning-10 px-2 py-0.5 text-200 font-normal whitespace-nowrap text-text-primary"
  >
    <span className="size-2 shrink-0 animate-pulse rounded-full bg-intent-warning-fill" />
    {count === 1 ? "Rollout in progress" : `${count} rollouts in progress`}
  </span>
);

const CollapseChevron = ({ expanded }: { expanded: boolean }) => (
  <ChevronDown width="w-3.5" className={clsx("shrink-0 transition-transform", !expanded && "-rotate-90")} />
);

// Compact in-group progress row. The full live view for the rollout sits in
// the "Active rollouts" section at the top of the tab; this row keeps
// progress visible in context, including while the group is collapsed.
const ModelGroupRolloutProgress = ({ rollout }: { rollout: Rollout }) => {
  const counts = rolloutDeviceCounts(rollout);
  return (
    <div className="flex flex-col gap-1.5" data-testid={`model-group-rollout-progress-${rollout.model}`}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-200 text-text-primary-70">
        <span>{`Rolling out ${rollout.firmwareVersion}`}</span>
        <span>{rolloutProgressSummary(counts)}</span>
      </div>
      <CompositionBar segments={rolloutProgressSegments(counts)} height={6} colorMap={rolloutProgressColorMap} />
    </div>
  );
};

interface ModelGroupSectionProps {
  group: RolloutLaneModelGroup;
  activeRollout: Rollout | undefined;
  firmwareFiles: FirmwareFileInfo[];
  stagedFileId: string;
  onStageFirmware: (model: string, fileId: string) => void;
  onViewMiners: () => void;
}

// Compact per-model row: identity on the left, actions on the right, and
// live rollout progress below. The miner table lives in a modal behind
// "View miners" so lanes with many miners stay scannable.
const ModelGroupSection = ({
  group,
  activeRollout,
  firmwareFiles,
  stagedFileId,
  onStageFirmware,
  onViewMiners,
}: ModelGroupSectionProps) => {
  const options = useMemo(() => {
    const matching = firmwareFiles.filter((f) => f.target_model.toLowerCase() === group.model.toLowerCase());
    return [
      { value: "", label: "No firmware" },
      ...matching.map((f) => ({
        value: f.id,
        label: f.firmware_version || f.filename,
        description: f.filename,
      })),
    ];
  }, [firmwareFiles, group.model]);

  return (
    <div className="flex flex-col gap-3 rounded-lg bg-core-primary-5 p-4" data-testid={`model-group-${group.model}`}>
      <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
        <div className="flex items-center gap-3">
          <span className="text-heading-100 text-text-primary">{group.model || "Unknown model"}</span>
          <span className="text-200 text-text-primary-50">
            {group.miners.length === 1 ? "1 miner" : `${group.miners.length} miners`}
          </span>
        </div>
        <div className="flex items-center gap-3 phone:flex-col phone:items-stretch">
          {group.miners.length > 0 ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="View miners"
              onClick={onViewMiners}
              testId={`view-miners-${group.model}`}
            />
          ) : null}
          <FirmwarePickerButton
            label={`Firmware for ${group.model || "unknown model"}`}
            options={options}
            value={stagedFileId}
            onChange={(value) => onStageFirmware(group.model, value)}
            testId={`lane-firmware-select-${group.model}`}
          />
        </div>
      </div>

      {activeRollout ? <ModelGroupRolloutProgress rollout={activeRollout} /> : null}

      {group.miners.length === 0 ? (
        <span className="text-200 text-text-primary-50">
          No miners of this model in the channel. The assignment applies as soon as one is added.
        </span>
      ) : null}
    </div>
  );
};

interface LaneCardProps {
  lane: RolloutLane;
  rollouts: Rollout[];
  firmwareFiles: FirmwareFileInfo[];
  minerNames: Record<string, string>;
  onManageMiners: (lane: RolloutLane) => void;
  onShowHistory: (lane: RolloutLane) => void;
  onDelete: (lane: RolloutLane) => void;
  onApply: (laneId: bigint, assignments: { model: string; firmwareFileId: string }[]) => Promise<void>;
}

// Exported for Storybook; the tab below is the only production consumer.
export const LaneCard = ({
  lane,
  rollouts,
  firmwareFiles,
  minerNames,
  onManageMiners,
  onShowHistory,
  onDelete,
  onApply,
}: LaneCardProps) => {
  // Staged (unapplied) firmware choices per model; absent key = server value.
  const [staged, setStaged] = useState<Record<string, string>>({});
  const [isApplying, setIsApplying] = useState(false);
  const [expanded, setExpanded] = useState(true);
  // Model whose miner table is open in the "View miners" modal.
  const [minersModel, setMinersModel] = useState<string | null>(null);

  const memberCount = lane.modelGroups.reduce((sum, group) => sum + group.miners.length, 0);
  const laneRollouts = rollouts.filter((r) => r.laneId === lane.id);
  const activeByModel = new Map(laneRollouts.filter((r) => r.status === RolloutStatus.ACTIVE).map((r) => [r.model, r]));
  const activeCount = activeByModel.size;
  // Derived from the polled lane on every render so the open modal tracks
  // live firmware versions and rollout states; closes if the group empties.
  const minersGroup = minersModel !== null ? lane.modelGroups.find((group) => group.model === minersModel) : undefined;

  const stagedValue = (group: RolloutLaneModelGroup): string =>
    staged[group.model] !== undefined ? staged[group.model] : group.firmwareFileId;

  const dirtyAssignments = lane.modelGroups
    .filter((group) => stagedValue(group) !== group.firmwareFileId)
    .map((group) => ({ model: group.model, firmwareFileId: stagedValue(group) }));

  const handleApply = () => {
    setIsApplying(true);
    onApply(lane.id, dirtyAssignments)
      .then(() => {
        setStaged({});
        pushToast({ message: "Firmware changes applied", status: STATUSES.success });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't apply firmware changes", status: STATUSES.error });
      })
      .finally(() => setIsApplying(false));
  };

  return (
    <Card
      title={
        <button
          type="button"
          aria-expanded={expanded}
          data-testid="lane-toggle"
          className="flex cursor-pointer items-center gap-3 text-left"
          onClick={() => setExpanded((current) => !current)}
        >
          <CollapseChevron expanded={expanded} />
          <span>{lane.name}</span>
          <span className="text-200 font-normal text-text-primary-50">
            {memberCount === 1 ? "1 miner" : `${memberCount} miners`}
          </span>
          {activeCount > 0 ? <RolloutActivePill count={activeCount} testId="lane-rollout-pill" /> : null}
        </button>
      }
      type={cardType.default}
      testId={`rollout-lane-${lane.name}`}
      className={clsx(activeCount > 0 && "ring-1 ring-intent-warning-50")}
      headerAction={
        <div className="flex gap-2">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="History"
            onClick={() => onShowHistory(lane)}
          />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Manage miners"
            onClick={() => onManageMiners(lane)}
          />
          <Button variant={variants.danger} size={sizes.compact} text="Delete" onClick={() => onDelete(lane)} />
        </div>
      }
      bodyClassName={clsx("flex flex-col gap-4 p-6 pt-4", !expanded && "hidden")}
    >
      {lane.modelGroups.length === 0 ? (
        <span className="text-300 text-text-primary-50">
          Empty channel. Add miners to group them by model and assign firmware.
        </span>
      ) : (
        lane.modelGroups.map((group) => (
          <ModelGroupSection
            key={group.model}
            group={group}
            activeRollout={activeByModel.get(group.model)}
            firmwareFiles={firmwareFiles}
            stagedFileId={stagedValue(group)}
            onStageFirmware={(model, fileId) => setStaged((prev) => ({ ...prev, [model]: fileId }))}
            onViewMiners={() => setMinersModel(group.model)}
          />
        ))
      )}

      {minersGroup ? (
        <ModelMinersModal
          laneName={lane.name}
          group={minersGroup}
          activeRollout={activeByModel.get(minersGroup.model)}
          minerNames={minerNames}
          onClose={() => setMinersModel(null)}
        />
      ) : null}

      {dirtyAssignments.length > 0 ? (
        <div className="flex items-center justify-between gap-4 rounded-lg bg-intent-warning-10 px-4 py-3">
          <span className="text-300 text-text-primary">
            {dirtyAssignments.length === 1
              ? "1 firmware change pending"
              : `${dirtyAssignments.length} firmware changes pending`}
            {" — applying starts a rollout per model."}
          </span>
          <div className="flex shrink-0 gap-2">
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Discard"
              disabled={isApplying}
              onClick={() => setStaged({})}
            />
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Apply changes"
              loading={isApplying}
              onClick={handleApply}
            />
          </div>
        </div>
      ) : null}
    </Card>
  );
};

const RolloutLanesTab = () => {
  const {
    lanes,
    rollouts,
    minerNames,
    isLoading,
    createLane,
    deleteLane,
    updateMembers,
    applyFirmware,
    rollbackFirmware,
  } = useRolloutLanes();
  const { listFirmwareFiles } = useFirmwareApi();
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [newLaneName, setNewLaneName] = useState("");
  const [isCreating, setIsCreating] = useState(false);
  const [laneToDelete, setLaneToDelete] = useState<RolloutLane | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [laneToManage, setLaneToManage] = useState<RolloutLane | null>(null);
  const [isSavingMembers, setIsSavingMembers] = useState(false);
  const [laneForHistory, setLaneForHistory] = useState<RolloutLane | null>(null);
  // History entry whose firmware is about to be restored.
  const [rollbackTarget, setRollbackTarget] = useState<Rollout | null>(null);
  const [isRollingBack, setIsRollingBack] = useState(false);

  useEffect(() => {
    listFirmwareFiles()
      .then(setFirmwareFiles)
      .catch((error) => {
        pushToast({ message: error?.message || "Failed to load firmware files", status: STATUSES.error });
      });
  }, [listFirmwareFiles]);

  // Live views for ongoing rollouts, most recently started first.
  const activeRollouts = useMemo(
    () =>
      rollouts
        .filter((rollout) => rollout.status === RolloutStatus.ACTIVE)
        .sort((a, b) => (b.createdAt ? timestampMs(b.createdAt) : 0) - (a.createdAt ? timestampMs(a.createdAt) : 0)),
    [rollouts],
  );

  const handleCreate = () => {
    setIsCreating(true);
    createLane(newLaneName)
      .then(() => {
        setShowCreateDialog(false);
        setNewLaneName("");
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't create channel", status: STATUSES.error });
      })
      .finally(() => setIsCreating(false));
  };

  const handleRollback = () => {
    if (!rollbackTarget) return;
    const rollout = rollbackTarget;
    setIsRollingBack(true);
    rollbackFirmware(rollout.id)
      .then(() => {
        setRollbackTarget(null);
        pushToast({
          message: `Rolling back ${rollout.model} in ${rollout.laneName} to ${rollout.firmwareVersion}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't roll back firmware", status: STATUSES.error });
      })
      .finally(() => setIsRollingBack(false));
  };

  const handleDelete = () => {
    if (!laneToDelete) return;
    setIsDeleting(true);
    deleteLane(laneToDelete.id)
      .then(() => {
        pushToast({ message: `Deleted channel ${laneToDelete.name}`, status: STATUSES.success });
        setLaneToDelete(null);
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't delete channel", status: STATUSES.error });
      })
      .finally(() => setIsDeleting(false));
  };

  const laneMemberIdentifiers = useCallback(
    (lane: RolloutLane) => lane.modelGroups.flatMap((group) => group.miners.map((m) => m.deviceIdentifier)),
    [],
  );

  const handleSaveMembers = (selectedIdentifiers: string[]) => {
    if (!laneToManage) return;
    const before = new Set(laneMemberIdentifiers(laneToManage));
    const after = new Set(selectedIdentifiers);
    const add = selectedIdentifiers.filter((id) => !before.has(id));
    const remove = [...before].filter((id) => !after.has(id));
    if (add.length === 0 && remove.length === 0) {
      setLaneToManage(null);
      return;
    }
    setIsSavingMembers(true);
    updateMembers(laneToManage.id, add, remove)
      .then(() => {
        setLaneToManage(null);
        pushToast({ message: "Channel miners updated", status: STATUSES.success });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't update channel miners", status: STATUSES.error });
      })
      .finally(() => setIsSavingMembers(false));
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader title="Rollout channels" description={ROLLOUT_LANES_DESCRIPTION} />
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="New channel"
          onClick={() => setShowCreateDialog(true)}
          className="shrink-0 phone:w-full"
        />
      </div>

      {activeRollouts.length > 0 ? (
        <section className="grid gap-3" data-testid="active-rollouts-section">
          <Header
            title={activeRollouts.length === 1 ? "Active rollout" : "Active rollouts"}
            titleSize="text-heading-200"
          />
          {activeRollouts.map((rollout) => (
            <ModelRolloutStatus key={rollout.id.toString()} rollout={rollout} />
          ))}
        </section>
      ) : null}

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading rollout channels...</div>
      ) : lanes.length === 0 ? (
        <SettingsEmptyState
          title="No rollout channels"
          description="Create a channel, add miners to it, and assign firmware per model to roll out updates."
        />
      ) : (
        lanes.map((lane) => (
          <LaneCard
            key={lane.id.toString()}
            lane={lane}
            rollouts={rollouts}
            firmwareFiles={firmwareFiles}
            minerNames={minerNames}
            onManageMiners={setLaneToManage}
            onShowHistory={setLaneForHistory}
            onDelete={setLaneToDelete}
            onApply={applyFirmware}
          />
        ))
      )}

      <Dialog
        open={showCreateDialog}
        title="New rollout channel"
        subtitle="A channel starts empty. Add miners and assign firmware afterwards."
        testId="create-lane-dialog"
        onDismiss={() => {
          if (!isCreating) setShowCreateDialog(false);
        }}
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: () => setShowCreateDialog(false),
            disabled: isCreating,
          },
          {
            text: "Create channel",
            variant: variants.primary,
            onClick: handleCreate,
            loading: isCreating,
            disabled: newLaneName.trim() === "",
          },
        ]}
      >
        <Input
          id="lane-name"
          label="Channel name"
          initValue={newLaneName}
          onChange={(value) => setNewLaneName(value)}
          autoFocus
        />
      </Dialog>

      <Dialog
        open={laneToDelete !== null}
        title="Delete rollout channel?"
        subtitle={`Miners in ${laneToDelete?.name ?? "this channel"} are released and firmware is no longer enforced for them.`}
        testId="delete-lane-dialog"
        onDismiss={() => {
          if (!isDeleting) setLaneToDelete(null);
        }}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: () => setLaneToDelete(null),
            disabled: isDeleting,
          },
          {
            text: "Delete channel",
            variant: variants.danger,
            onClick: handleDelete,
            loading: isDeleting,
          },
        ]}
      />

      <Dialog
        open={rollbackTarget !== null}
        title="Roll back firmware?"
        subtitle={
          rollbackTarget
            ? `${rollbackTarget.model} in ${rollbackTarget.laneName} goes back to ${rollbackTarget.firmwareVersion}. Any in-progress rollout for this model is canceled and a new rollout restores this version.`
            : ""
        }
        testId="rollback-firmware-dialog"
        onDismiss={() => {
          if (!isRollingBack) setRollbackTarget(null);
        }}
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: () => setRollbackTarget(null),
            disabled: isRollingBack,
          },
          {
            text: "Roll back",
            variant: variants.primary,
            onClick: handleRollback,
            loading: isRollingBack,
          },
        ]}
      />

      {laneToManage ? (
        <MinerSelectionModal
          open
          selectedMinerIds={laneMemberIdentifiers(laneToManage)}
          onDismiss={() => {
            if (!isSavingMembers) setLaneToManage(null);
          }}
          onSave={(selection) => handleSaveMembers(selection.selectedMinerIds)}
        />
      ) : null}

      {laneForHistory ? (
        <LaneHistoryModal
          // Resolve the lane fresh on every poll so rollback eligibility in
          // the history rows tracks the current assignments.
          lane={lanes.find((lane) => lane.id === laneForHistory.id) ?? laneForHistory}
          rollouts={rollouts.filter((rollout) => rollout.laneId === laneForHistory.id)}
          onRollback={setRollbackTarget}
          onClose={() => setLaneForHistory(null)}
        />
      ) : null}
    </div>
  );
};

export default RolloutLanesTab;
