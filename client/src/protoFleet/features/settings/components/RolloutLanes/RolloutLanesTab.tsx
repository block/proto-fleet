import { useCallback, useEffect, useState } from "react";

import ChannelManageView from "./ChannelManageView";
import ChannelsTable from "./ChannelsTable";
import LaneHistoryModal from "./LaneHistoryModal";
import { Rollout, RolloutLane } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import type { RolloutLanesApi } from "@/protoFleet/api/useRolloutLanes";
import MinerSelectionModal from "@/protoFleet/components/TargetSelectionModal/MinerSelectionModal";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const ROLLOUT_LANES_DESCRIPTION =
  "Group miners into release channels and assign firmware per model. Assigned firmware is enforced: miners not on the assigned version are updated automatically.";

interface RolloutLanesTabProps {
  // Shared with the active-updates monitor above the tabs, so one poll
  // feeds both.
  api: RolloutLanesApi;
  // Channel to open in the manage view on mount (e.g. from an update's
  // "Manage" action).
  initialManagedLaneId?: bigint | null;
}

const RolloutLanesTab = ({ api, initialManagedLaneId = null }: RolloutLanesTabProps) => {
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
  } = api;
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
  // Channel drilled into via "Manage"; null shows the channels table.
  const [managedLaneId, setManagedLaneId] = useState<bigint | null>(initialManagedLaneId);

  useEffect(() => {
    listFirmwareFiles()
      .then(setFirmwareFiles)
      .catch((error) => {
        pushToast({ message: error?.message || "Failed to load firmware files", status: STATUSES.error });
      });
  }, [listFirmwareFiles]);

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
        setManagedLaneId(null);
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

  // Resolved fresh on every poll so the drill-in card tracks live progress;
  // falls back to the table if the channel goes.
  const managedLane = managedLaneId !== null ? lanes.find((lane) => lane.id === managedLaneId) : undefined;

  return (
    <div className="flex flex-col gap-6">
      <SettingsPageHeader title="Release channels" description={ROLLOUT_LANES_DESCRIPTION} />

      {managedLane ? (
        <>
          <button
            type="button"
            data-testid="back-to-channels"
            className="flex cursor-pointer items-center gap-2 self-start text-200 text-text-primary-70 transition-colors hover:text-text-primary"
            onClick={() => setManagedLaneId(null)}
          >
            ← All release channels
          </button>
          <ChannelManageView
            lane={managedLane}
            rollouts={rollouts}
            firmwareFiles={firmwareFiles}
            minerNames={minerNames}
            onManageMiners={setLaneToManage}
            onShowHistory={setLaneForHistory}
            onDelete={setLaneToDelete}
            onApply={applyFirmware}
          />
        </>
      ) : isLoading ? (
        <div className="text-center text-text-primary-50">Loading release channels...</div>
      ) : lanes.length === 0 ? (
        <div className="flex flex-col gap-6">
          <div>
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Create release channel"
              onClick={() => setShowCreateDialog(true)}
              className="phone:w-full"
            />
          </div>
          <SettingsEmptyState
            title="No release channels"
            description="Create a release channel, add miners to it, and assign firmware per model to roll out updates."
          />
        </div>
      ) : (
        <ChannelsTable
          lanes={lanes}
          rollouts={rollouts}
          onCreate={() => setShowCreateDialog(true)}
          onManage={(lane) => setManagedLaneId(lane.id)}
        />
      )}

      <Dialog
        open={showCreateDialog}
        title="New release channel"
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
        title="Delete release channel?"
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
