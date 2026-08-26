import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import {
  Rollout,
  RolloutDeviceState,
  RolloutLane,
  RolloutLaneModelGroup,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { useRolloutLanes } from "@/protoFleet/api/useRolloutLanes";
import MinerSelectionModal from "@/protoFleet/components/TargetSelectionModal/MinerSelectionModal";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const ROLLOUT_LANES_DESCRIPTION =
  "Group miners into lanes and assign firmware per model. Assigned firmware is enforced: miners not on the assigned version are updated automatically.";

const deviceStateLabels: Record<RolloutDeviceState, string> = {
  [RolloutDeviceState.UNSPECIFIED]: "",
  [RolloutDeviceState.PENDING]: "Pending",
  [RolloutDeviceState.UPDATING]: "Updating",
  [RolloutDeviceState.UPDATED]: "Updated",
};

const rolloutStatusLabels: Record<RolloutStatus, string> = {
  [RolloutStatus.UNSPECIFIED]: "Unknown",
  [RolloutStatus.ACTIVE]: "In progress",
  [RolloutStatus.COMPLETED]: "Completed",
  [RolloutStatus.CANCELED]: "Canceled",
};

const StatusChip = ({ label, tone }: { label: string; tone: "neutral" | "progress" | "success" | "critical" }) => (
  <span
    className={clsx(
      "inline-flex items-center rounded-full px-2 py-0.5 text-200 whitespace-nowrap",
      tone === "success" && "bg-intent-success-10 text-text-primary",
      tone === "progress" && "bg-intent-warning-10 text-text-primary",
      tone === "critical" && "bg-intent-critical-10 text-text-critical",
      tone === "neutral" && "bg-core-primary-5 text-text-primary-70",
    )}
  >
    {label}
  </span>
);

const deviceStateTone = (state: RolloutDeviceState): "neutral" | "progress" | "success" => {
  if (state === RolloutDeviceState.UPDATED) return "success";
  if (state === RolloutDeviceState.UPDATING) return "progress";
  return "neutral";
};

const RolloutProgress = ({ rollout }: { rollout: Rollout }) => {
  const total = rollout.devices.length;
  const updated = rollout.devices.filter((d) => d.state === RolloutDeviceState.UPDATED).length;
  const percent = total === 0 ? 0 : Math.round((updated / total) * 100);
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between text-200 text-text-primary-70">
        <span>{`Rolling out ${rollout.firmwareVersion}`}</span>
        <span>{`${updated}/${total} updated`}</span>
      </div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-core-primary-5">
        <div className="h-full rounded-full bg-intent-success-fill transition-all" style={{ width: `${percent}%` }} />
      </div>
    </div>
  );
};

interface ModelGroupSectionProps {
  group: RolloutLaneModelGroup;
  activeRollout: Rollout | undefined;
  firmwareFiles: FirmwareFileInfo[];
  minerNames: Record<string, string>;
  stagedFileId: string;
  onStageFirmware: (model: string, fileId: string) => void;
}

const ModelGroupSection = ({
  group,
  activeRollout,
  firmwareFiles,
  minerNames,
  stagedFileId,
  onStageFirmware,
}: ModelGroupSectionProps) => {
  const options = useMemo(() => {
    const matching = firmwareFiles.filter((f) => f.target_model.toLowerCase() === group.model.toLowerCase());
    return [
      { value: "", label: "No firmware" },
      ...matching.map((f) => ({
        value: f.id,
        label: f.firmware_version ? `${f.firmware_version} (${f.filename})` : f.filename,
      })),
    ];
  }, [firmwareFiles, group.model]);

  const deviceStates = useMemo(() => {
    const states: Record<string, RolloutDeviceState> = {};
    for (const device of activeRollout?.devices ?? []) {
      states[device.deviceIdentifier] = device.state;
    }
    return states;
  }, [activeRollout]);

  return (
    <div className="flex flex-col gap-3 rounded-lg bg-core-primary-5 p-4">
      <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
        <div className="flex items-center gap-3">
          <span className="text-heading-100 text-text-primary">{group.model || "Unknown model"}</span>
          <span className="text-200 text-text-primary-50">
            {group.miners.length === 1 ? "1 miner" : `${group.miners.length} miners`}
          </span>
        </div>
        <div className="w-72 phone:w-full">
          <Select
            id={`firmware-${group.model}`}
            label="Firmware"
            testId={`lane-firmware-select-${group.model}`}
            options={options}
            value={stagedFileId}
            onChange={(value) => onStageFirmware(group.model, value)}
            emptyMessage="No uploaded firmware targets this model"
          />
        </div>
      </div>

      {activeRollout ? <RolloutProgress rollout={activeRollout} /> : null}

      {group.miners.length > 0 ? (
        <table className="w-full text-left text-200">
          <thead>
            <tr className="text-text-primary-50">
              <th className="py-1 pr-4 font-normal">Miner</th>
              <th className="py-1 pr-4 font-normal">Current firmware</th>
              <th className="py-1 font-normal">Status</th>
            </tr>
          </thead>
          <tbody className="text-text-primary">
            {group.miners.map((miner) => {
              const state = deviceStates[miner.deviceIdentifier];
              const onTarget = group.firmwareVersion !== "" && miner.firmwareVersion === group.firmwareVersion;
              return (
                <tr key={miner.deviceIdentifier} data-testid={`lane-miner-${miner.deviceIdentifier}`}>
                  <td className="py-1 pr-4">{minerNames[miner.deviceIdentifier] || miner.deviceIdentifier}</td>
                  <td className="py-1 pr-4">{miner.firmwareVersion || "Unknown"}</td>
                  <td className="py-1">
                    {state !== undefined && state !== RolloutDeviceState.UNSPECIFIED ? (
                      <StatusChip label={deviceStateLabels[state]} tone={deviceStateTone(state)} />
                    ) : onTarget ? (
                      <StatusChip label="On assigned version" tone="success" />
                    ) : group.firmwareVersion !== "" ? (
                      <StatusChip label="Not on assigned version" tone="neutral" />
                    ) : (
                      <span className="text-text-primary-50">—</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      ) : (
        <span className="text-200 text-text-primary-50">
          No miners of this model in the lane. The assignment applies as soon as one is added.
        </span>
      )}
    </div>
  );
};

interface LaneCardProps {
  lane: RolloutLane;
  rollouts: Rollout[];
  firmwareFiles: FirmwareFileInfo[];
  minerNames: Record<string, string>;
  onManageMiners: (lane: RolloutLane) => void;
  onDelete: (lane: RolloutLane) => void;
  onApply: (laneId: bigint, assignments: { model: string; firmwareFileId: string }[]) => Promise<void>;
}

const LaneCard = ({ lane, rollouts, firmwareFiles, minerNames, onManageMiners, onDelete, onApply }: LaneCardProps) => {
  // Staged (unapplied) firmware choices per model; absent key = server value.
  const [staged, setStaged] = useState<Record<string, string>>({});
  const [isApplying, setIsApplying] = useState(false);

  const memberCount = lane.modelGroups.reduce((sum, group) => sum + group.miners.length, 0);
  const laneRollouts = rollouts.filter((r) => r.laneId === lane.id);
  const activeByModel = new Map(laneRollouts.filter((r) => r.status === RolloutStatus.ACTIVE).map((r) => [r.model, r]));
  const history = laneRollouts.filter((r) => r.status !== RolloutStatus.ACTIVE).slice(0, 5);

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
        <div className="flex items-baseline gap-3">
          <span>{lane.name}</span>
          <span className="text-200 font-normal text-text-primary-50">
            {memberCount === 1 ? "1 miner" : `${memberCount} miners`}
          </span>
        </div>
      }
      type={cardType.default}
      testId={`rollout-lane-${lane.name}`}
      headerAction={
        <div className="flex gap-2">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Manage miners"
            onClick={() => onManageMiners(lane)}
          />
          <Button variant={variants.danger} size={sizes.compact} text="Delete" onClick={() => onDelete(lane)} />
        </div>
      }
      bodyClassName="flex flex-col gap-4 p-6 pt-4"
    >
      {lane.modelGroups.length === 0 ? (
        <span className="text-300 text-text-primary-50">
          Empty lane. Add miners to group them by model and assign firmware.
        </span>
      ) : (
        lane.modelGroups.map((group) => (
          <ModelGroupSection
            key={group.model}
            group={group}
            activeRollout={activeByModel.get(group.model)}
            firmwareFiles={firmwareFiles}
            minerNames={minerNames}
            stagedFileId={stagedValue(group)}
            onStageFirmware={(model, fileId) => setStaged((prev) => ({ ...prev, [model]: fileId }))}
          />
        ))
      )}

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
              text={isApplying ? "Applying..." : "Apply changes"}
              disabled={isApplying}
              onClick={handleApply}
            />
          </div>
        </div>
      ) : null}

      {history.length > 0 ? (
        <div className="flex flex-col gap-2">
          <span className="text-200 text-text-primary-50">Recent rollouts</span>
          {history.map((rollout) => (
            <div key={rollout.id.toString()} className="flex items-center gap-3 text-200 text-text-primary-70">
              <StatusChip
                label={rolloutStatusLabels[rollout.status]}
                tone={rollout.status === RolloutStatus.COMPLETED ? "success" : "neutral"}
              />
              <span>{`${rollout.model} → ${rollout.firmwareVersion}`}</span>
            </div>
          ))}
        </div>
      ) : null}
    </Card>
  );
};

const RolloutLanesTab = () => {
  const { lanes, rollouts, minerNames, isLoading, createLane, deleteLane, updateMembers, applyFirmware } =
    useRolloutLanes();
  const { listFirmwareFiles } = useFirmwareApi();
  const [firmwareFiles, setFirmwareFiles] = useState<FirmwareFileInfo[]>([]);
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [newLaneName, setNewLaneName] = useState("");
  const [isCreating, setIsCreating] = useState(false);
  const [laneToDelete, setLaneToDelete] = useState<RolloutLane | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [laneToManage, setLaneToManage] = useState<RolloutLane | null>(null);
  const [isSavingMembers, setIsSavingMembers] = useState(false);

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
        pushToast({ message: error?.message || "Couldn't create lane", status: STATUSES.error });
      })
      .finally(() => setIsCreating(false));
  };

  const handleDelete = () => {
    if (!laneToDelete) return;
    setIsDeleting(true);
    deleteLane(laneToDelete.id)
      .then(() => {
        pushToast({ message: `Deleted lane ${laneToDelete.name}`, status: STATUSES.success });
        setLaneToDelete(null);
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't delete lane", status: STATUSES.error });
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
        pushToast({ message: "Lane miners updated", status: STATUSES.success });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't update lane miners", status: STATUSES.error });
      })
      .finally(() => setIsSavingMembers(false));
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <SettingsPageHeader title="Rollout lanes" description={ROLLOUT_LANES_DESCRIPTION} />
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="New lane"
          onClick={() => setShowCreateDialog(true)}
          className="shrink-0 phone:w-full"
        />
      </div>

      {isLoading ? (
        <div className="text-center text-text-primary-50">Loading rollout lanes...</div>
      ) : lanes.length === 0 ? (
        <SettingsEmptyState
          title="No rollout lanes"
          description="Create a lane, add miners to it, and assign firmware per model to roll out updates."
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
            onDelete={setLaneToDelete}
            onApply={applyFirmware}
          />
        ))
      )}

      <Dialog
        open={showCreateDialog}
        title="New rollout lane"
        subtitle="A lane starts empty. Add miners and assign firmware afterwards."
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
            text: isCreating ? "Creating..." : "Create lane",
            variant: variants.primary,
            onClick: handleCreate,
            disabled: isCreating || newLaneName.trim() === "",
          },
        ]}
      >
        <Input
          id="lane-name"
          label="Lane name"
          initValue={newLaneName}
          onChange={(value) => setNewLaneName(value)}
          autoFocus
        />
      </Dialog>

      <Dialog
        open={laneToDelete !== null}
        title="Delete rollout lane?"
        subtitle={`Miners in ${laneToDelete?.name ?? "this lane"} are released and firmware is no longer enforced for them.`}
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
            text: isDeleting ? "Deleting..." : "Delete lane",
            variant: variants.danger,
            onClick: handleDelete,
            disabled: isDeleting,
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
    </div>
  );
};

export default RolloutLanesTab;
