import { useMemo, useState } from "react";

import { ModelStatusCell } from "./channelStatus";
import FirmwarePickerButton from "./FirmwarePickerButton";
import ModelMinersModal from "./ModelMinersModal";
import {
  isPaused,
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
} from "./rolloutStatus";
import {
  type Rollout,
  type RolloutLane,
  type RolloutLaneModelGroup,
  RolloutMethod,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import { type ApplyRolloutOptions, IMMEDIATE_ROLLOUT_OPTIONS } from "@/protoFleet/api/useRolloutLanes";
import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import CompositionBar from "@/shared/components/CompositionBar";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Radio from "@/shared/components/Radio";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Attention pill with a pulsing dot, shown while a rollout is ongoing.
const RolloutActivePill = ({ count, testId }: { count: number; testId?: string }) => (
  <span
    data-testid={testId}
    className="inline-flex items-center gap-1.5 rounded-full bg-intent-warning-10 px-2 py-0.5 text-200 font-normal whitespace-nowrap text-text-primary"
  >
    <span className="size-2 shrink-0 animate-pulse rounded-full bg-intent-warning-fill" />
    {count === 1 ? "Rollout in progress" : `${count} rollouts in progress`}
  </span>
);

interface FirmwarePickerCellProps {
  group: RolloutLaneModelGroup;
  firmwareFiles: FirmwareFileInfo[];
  stagedFileId: string;
  onStageFirmware: (model: string, fileId: string) => void;
}

const FirmwarePickerCell = ({ group, firmwareFiles, stagedFileId, onStageFirmware }: FirmwarePickerCellProps) => {
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
    <FirmwarePickerButton
      label={`Firmware for ${group.model || "unknown model"}`}
      options={options}
      value={stagedFileId}
      onChange={(value) => onStageFirmware(group.model, value)}
      testId={`lane-firmware-select-${group.model}`}
    />
  );
};

interface ChannelManageViewProps {
  lane: RolloutLane;
  rollouts: Rollout[];
  firmwareFiles: FirmwareFileInfo[];
  minerNames: Record<string, string>;
  onManageMiners: (lane: RolloutLane) => void;
  onShowHistory: (lane: RolloutLane) => void;
  onDelete: (lane: RolloutLane) => void;
  onApply: (
    laneId: bigint,
    assignments: { model: string; firmwareFileId: string }[],
    options?: ApplyRolloutOptions,
  ) => Promise<void>;
}

// The per-channel management surface behind "Manage": a flat per-model table
// in the release channels design language, with the firmware picker and
// update status per row, live rollout progress beneath updating rows, and
// channel-level actions in the header.
const ChannelManageView = ({
  lane,
  rollouts,
  firmwareFiles,
  minerNames,
  onManageMiners,
  onShowHistory,
  onDelete,
  onApply,
}: ChannelManageViewProps) => {
  // Staged (unapplied) firmware choices per model; absent key = server value.
  const [staged, setStaged] = useState<Record<string, string>>({});
  const [isApplying, setIsApplying] = useState(false);
  // Model whose miner table is open in the "View miners" modal.
  const [minersModel, setMinersModel] = useState<string | null>(null);
  // Apply confirmation: rollout method, batch size and auto-advance
  // thresholds for this apply call.
  const [showApplyDialog, setShowApplyDialog] = useState(false);
  const [applyMethod, setApplyMethod] = useState<RolloutMethod>(RolloutMethod.IMMEDIATE);
  const [batchSizeText, setBatchSizeText] = useState("1");
  const [autoAdvance, setAutoAdvance] = useState(false);
  const [maxDropText, setMaxDropText] = useState("10");
  const [stabilizationMinutesText, setStabilizationMinutesText] = useState("10");

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

  // Human-readable version for a staged file id, for the dialog summary.
  const versionLabel = (fileId: string): string => {
    if (fileId === "") return "no firmware";
    const file = firmwareFiles.find((f) => f.id === fileId);
    return file?.firmware_version || file?.filename || "unknown version";
  };

  const dirtyMinerCount = lane.modelGroups
    .filter((group) => stagedValue(group) !== group.firmwareFileId)
    .reduce((sum, group) => sum + group.miners.length, 0);

  const isStagedMethod = applyMethod !== RolloutMethod.IMMEDIATE;
  const batchSize = Math.max(1, Number.parseInt(batchSizeText, 10) || 1);
  const maxDrop = Math.min(100, Math.max(0, Number.parseFloat(maxDropText) || 0));
  const stabilizationMinutes = Math.max(0, Number.parseInt(stabilizationMinutesText, 10) || 0);

  const handleApply = () => {
    const options: ApplyRolloutOptions = isStagedMethod
      ? {
          method: applyMethod,
          batchSize,
          autoAdvance,
          maxHashrateDropPercent: autoAdvance ? maxDrop : 0,
          stabilizationSeconds: autoAdvance ? stabilizationMinutes * 60 : 0,
        }
      : IMMEDIATE_ROLLOUT_OPTIONS;
    setIsApplying(true);
    onApply(lane.id, dirtyAssignments, options)
      .then(() => {
        setStaged({});
        setShowApplyDialog(false);
        pushToast({ message: "Firmware changes applied", status: STATUSES.success });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't apply firmware changes", status: STATUSES.error });
      })
      .finally(() => setIsApplying(false));
  };

  return (
    <div className="flex flex-col gap-5" data-testid={`rollout-lane-${lane.name}`}>
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-3">
            <h2 className="text-heading-200 text-text-primary">{lane.name}</h2>
            {activeCount > 0 ? <RolloutActivePill count={activeCount} testId="lane-rollout-pill" /> : null}
          </div>
          <div className="flex items-center gap-2 text-200 text-text-primary-50">
            <span>{memberCount === 1 ? "1 miner" : `${memberCount} miners`}</span>
            <span>·</span>
            <span>{lane.modelGroups.length === 1 ? "1 model" : `${lane.modelGroups.length} models`}</span>
          </div>
        </div>
        <div className="flex gap-2 phone:flex-col phone:items-stretch">
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
      </div>

      {lane.modelGroups.length === 0 ? (
        <span className="text-300 text-text-primary-50">
          Empty channel. Add miners to group them by model and assign firmware.
        </span>
      ) : (
        <table className="w-full text-left text-200">
          <thead>
            <tr className="text-text-primary-50">
              <th className="py-1.5 pr-4 font-normal">Model</th>
              <th className="py-1.5 pr-4 font-normal">Miners</th>
              <th className="py-1.5 pr-4 font-normal">Firmware</th>
              <th className="py-1.5 pr-4 font-normal">Update status</th>
              <th className="py-1.5 font-normal">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody className="text-text-primary">
            {lane.modelGroups.map((group) => {
              const activeRollout = activeByModel.get(group.model);
              const counts = activeRollout ? rolloutDeviceCounts(activeRollout) : undefined;
              return [
                <tr key={group.model} className="border-t border-border-5" data-testid={`model-group-${group.model}`}>
                  <td className="py-3 pr-4 text-emphasis-300">{group.model || "Unknown model"}</td>
                  <td className="py-3 pr-4">{group.miners.length.toLocaleString()}</td>
                  <td className="py-3 pr-4">
                    <FirmwarePickerCell
                      group={group}
                      firmwareFiles={firmwareFiles}
                      stagedFileId={stagedValue(group)}
                      onStageFirmware={(model, fileId) => setStaged((prev) => ({ ...prev, [model]: fileId }))}
                    />
                  </td>
                  <td className="py-3 pr-4">
                    <ModelStatusCell group={group} activeRollout={activeRollout} />
                  </td>
                  <td className="py-3 text-right">
                    {group.miners.length > 0 ? (
                      <Button
                        variant={variants.secondary}
                        size={sizes.compact}
                        text="View miners"
                        onClick={() => setMinersModel(group.model)}
                        testId={`view-miners-${group.model}`}
                      />
                    ) : null}
                  </td>
                </tr>,
                activeRollout && counts ? (
                  <tr key={`${group.model}-progress`} data-testid={`model-group-rollout-progress-${group.model}`}>
                    <td className="pb-3" colSpan={5}>
                      <div className="flex flex-col gap-1.5">
                        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-200 text-text-primary-70">
                          <span>
                            {isPaused(activeRollout)
                              ? `Rolling out ${activeRollout.firmwareVersion} (paused)`
                              : `Rolling out ${activeRollout.firmwareVersion}`}
                          </span>
                          <span>{rolloutProgressSummary(counts)}</span>
                        </div>
                        <CompositionBar
                          segments={rolloutProgressSegments(counts)}
                          height={6}
                          colorMap={rolloutProgressColorMap}
                        />
                      </div>
                    </td>
                  </tr>
                ) : null,
              ];
            })}
          </tbody>
        </table>
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
              onClick={() => setShowApplyDialog(true)}
            />
          </div>
        </div>
      ) : null}

      <Dialog
        open={showApplyDialog}
        title="Start firmware rollout?"
        subtitle={`One rollout starts per changed model in ${lane.name}.`}
        testId="apply-firmware-dialog"
        // Wider than the default dialog: three method choices with
        // descriptions plus the auto-advance controls need the room.
        className="!w-[42rem]"
        onDismiss={() => {
          if (!isApplying) setShowApplyDialog(false);
        }}
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: () => setShowApplyDialog(false),
            disabled: isApplying,
          },
          {
            text: "Start rollout",
            variant: variants.primary,
            onClick: handleApply,
            loading: isApplying,
          },
        ]}
      >
        <div className="flex flex-col gap-5">
          <div>
            {dirtyAssignments.map((assignment) => (
              <div
                key={assignment.model}
                className="flex items-baseline justify-between gap-4 border-t border-border-5 py-2.5 text-200"
              >
                <span className="text-text-primary">{assignment.model || "Unknown model"}</span>
                <span className="text-right text-text-primary-70">{versionLabel(assignment.firmwareFileId)}</span>
              </div>
            ))}
          </div>

          <div className="flex flex-col gap-3" role="radiogroup" aria-label="Rollout method">
            <label className="flex cursor-pointer items-start gap-3" data-testid="apply-method-immediate">
              <Radio
                className="mt-0.5 shrink-0"
                name="rollout-method"
                selected={applyMethod === RolloutMethod.IMMEDIATE}
                onChange={() => setApplyMethod(RolloutMethod.IMMEDIATE)}
              />
              <span className="flex flex-col">
                <span className="text-300 text-text-primary">All at once</span>
                <span className="text-200 text-text-primary-70">Every miner not on the new version updates now.</span>
              </span>
            </label>
            <label className="flex cursor-pointer items-start gap-3" data-testid="apply-method-pilot">
              <Radio
                className="mt-0.5 shrink-0"
                name="rollout-method"
                selected={applyMethod === RolloutMethod.PILOT}
                onChange={() => setApplyMethod(RolloutMethod.PILOT)}
              />
              <span className="flex flex-col">
                <span className="text-300 text-text-primary">Pilot first</span>
                <span className="text-200 text-text-primary-70">
                  A small group updates first. The rest wait until the pilot is reviewed and the rollout continued.
                </span>
              </span>
            </label>
            <label className="flex cursor-pointer items-start gap-3" data-testid="apply-method-batches">
              <Radio
                className="mt-0.5 shrink-0"
                name="rollout-method"
                selected={applyMethod === RolloutMethod.BATCHES}
                onChange={() => setApplyMethod(RolloutMethod.BATCHES)}
              />
              <span className="flex flex-col">
                <span className="text-300 text-text-primary">Fixed batches</span>
                <span className="text-200 text-text-primary-70">
                  Miners update in batches of a fixed size, with a review between every batch.
                </span>
              </span>
            </label>
          </div>

          {isStagedMethod ? (
            <div className="flex flex-col gap-4 rounded-lg bg-core-primary-5 p-4">
              <div className="flex flex-col gap-1">
                <Input
                  id="batch-size"
                  label={
                    applyMethod === RolloutMethod.PILOT
                      ? "Pilot size (miners per model)"
                      : "Batch size (miners per batch)"
                  }
                  type="number"
                  initValue={batchSizeText}
                  onChange={(value) => setBatchSizeText(value)}
                />
                {dirtyMinerCount > 0 ? (
                  <span className="text-200 text-text-primary-50">
                    {`${dirtyMinerCount.toLocaleString()} miners are affected in total.`}
                  </span>
                ) : null}
              </div>

              <label className="flex cursor-pointer items-start gap-3" data-testid="apply-auto-advance">
                <Checkbox
                  className="mt-0.5 shrink-0"
                  checked={autoAdvance}
                  onChange={(e) => setAutoAdvance(e.target.checked)}
                />
                <span className="flex flex-col">
                  <span className="text-300 text-text-primary">Continue automatically when the evidence holds</span>
                  <span className="text-200 text-text-primary-70">
                    Each review gate releases on its own once every miner in the batch is back and hashing, no new
                    errors appeared, hashrate stayed within the limit, and the stabilization period passed. Degraded or
                    missing evidence always waits for you.
                  </span>
                </span>
              </label>

              {autoAdvance ? (
                <div className="grid grid-cols-2 gap-3 phone:grid-cols-1">
                  <Input
                    id="max-hashrate-drop"
                    label="Max hashrate drop (%)"
                    type="number"
                    initValue={maxDropText}
                    onChange={(value) => setMaxDropText(value)}
                  />
                  <Input
                    id="stabilization-minutes"
                    label="Stabilization (minutes)"
                    type="number"
                    initValue={stabilizationMinutesText}
                    onChange={(value) => setStabilizationMinutesText(value)}
                  />
                </div>
              ) : null}
            </div>
          ) : null}
        </div>
      </Dialog>
    </div>
  );
};

export default ChannelManageView;
