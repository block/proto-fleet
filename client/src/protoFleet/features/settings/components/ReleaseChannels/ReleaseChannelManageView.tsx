import { type ReactElement, type ReactNode, useCallback, useMemo, useRef, useState } from "react";
import { create, equals } from "@bufbuild/protobuf";

import { defaultBehavior, rolloutSizeError } from "./behaviorUtils";
import { ModelStatusCell } from "./channelStatus";
import FirmwarePickerButton from "./FirmwarePickerButton";
import ModelMinersModal from "./ModelMinersModal";
import RolloutControls from "./RolloutControls";
import {
  isActive,
  isPaused,
  pacingSummary,
  pairKey,
  pairLabel,
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
} from "./rolloutStatus";
import ScopeEditor from "./ScopeEditor";
import {
  type PreviewReleaseChannelScopeResponse,
  type ReleaseChannelMiner,
  type ReleaseChannelModelGroup,
  type ReleaseChannelScope,
  ReleaseChannelScopeSchema,
  type Rollout,
  RolloutAutomationThresholdsSchema,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  type RolloutDevice,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { AssignmentDraft, ChannelView, ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar from "@/shared/components/CompositionBar";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Textarea from "@/shared/components/Textarea";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const MAX_FIRMWARE_ASSIGNMENTS = 100;
const assignmentLimitMessage =
  "Apply up to 100 model changes at a time. Revert some selections or discard them and choose fewer models.";

// Attention pill with a pulsing dot, shown while an update is ongoing.
const UpdateActivePill = ({ count, testId }: { count: number; testId?: string }) => (
  <span
    data-testid={testId}
    className="inline-flex items-center gap-1.5 rounded-full bg-intent-warning-10 px-2 py-0.5 text-200 font-normal whitespace-nowrap text-text-primary"
  >
    <span className="size-2 shrink-0 animate-pulse rounded-full bg-intent-warning-fill" />
    {count === 1 ? "Update in progress" : `${count} updates in progress`}
  </span>
);

function Section({ title, subtext, children }: { title: string; subtext?: string; children: ReactNode }): ReactElement {
  return (
    <section className="grid gap-3">
      <div className="grid">
        <div className="text-emphasis-300 text-text-primary">{title}</div>
        {subtext ? <div className="text-300 text-text-primary-70">{subtext}</div> : null}
      </div>
      {children}
    </section>
  );
}

// Observed groups retain spelling and whitespace for their rows and miner reads.
// Firmware assignments and rollout joins instead use the normalized pairKey.
const observedPairKey = (group: ReleaseChannelModelGroup): string => JSON.stringify([group.manufacturer, group.model]);

function behaviorForComparison(behavior: RolloutBehavior): RolloutBehavior {
  const effective = rolloutBehaviorForRequest(behavior);
  // The server omits empty thresholds. Explicit zero limits remain meaningful.
  if (
    effective.thresholds &&
    equals(RolloutAutomationThresholdsSchema, effective.thresholds, create(RolloutAutomationThresholdsSchema))
  ) {
    effective.thresholds = undefined;
  }
  return effective;
}

interface FirmwarePickerCellProps {
  group: ReleaseChannelModelGroup;
  firmwareFiles: FirmwareFileInfo[];
  stagedFileId: string | undefined;
  invalidSelection: boolean;
  onStageFirmware: (group: ReleaseChannelModelGroup, fileId: string) => void;
}

// Files whose target pair matches the group's observed pair under the
// server's ASCII fold; a group with an unknown identity matches nothing.
const filesForGroup = (firmwareFiles: FirmwareFileInfo[], group: ReleaseChannelModelGroup): FirmwareFileInfo[] => {
  const key = minerTargetKey(group.manufacturer, group.model);
  return key === null ? [] : firmwareFiles.filter((f) => minerTargetKey(f.target_manufacturer, f.target_model) === key);
};

const FirmwarePickerCell = ({
  group,
  firmwareFiles,
  stagedFileId,
  invalidSelection,
  onStageFirmware,
}: FirmwarePickerCellProps) => {
  const options = useMemo(
    () => [
      { value: "", label: "No firmware" },
      ...filesForGroup(firmwareFiles, group).map((f) => ({
        value: f.id,
        label: f.firmware_version || f.filename,
        description: f.filename,
      })),
    ],
    [firmwareFiles, group],
  );
  // The checksum identifies the assignment even when its uploaded file is gone.
  // Keep that state distinct from an explicitly staged clear (the empty string).
  const value = stagedFileId ?? (group.firmwareFileId || (group.firmwareChecksum ? null : ""));
  const unresolvedLabel =
    !invalidSelection && (stagedFileId === undefined || stagedFileId === group.firmwareFileId)
      ? group.firmwareVersion
      : undefined;

  return (
    <div className="grid gap-1">
      <FirmwarePickerButton
        label={`Firmware for ${pairLabel(group)}`}
        options={options}
        value={value}
        unresolvedLabel={unresolvedLabel}
        onChange={(value) => onStageFirmware(group, value)}
        testId={`channel-firmware-select-${group.model}`}
      />
      {invalidSelection ? (
        <p role="alert" className="text-200 text-intent-critical-fill">
          Selected firmware is unavailable for this model. Choose another version or discard the pending changes.
        </p>
      ) : null}
    </div>
  );
};

interface ReleaseChannelManageViewProps {
  // Undefined creates a new channel.
  channel?: ChannelView;
  hasRefreshError?: boolean;
  rollouts: Rollout[];
  firmwareFiles: FirmwareFileInfo[];
  minerNames: Record<string, string>;
  previewScope: (scope: ReleaseChannelScope, channelId?: bigint) => Promise<PreviewReleaseChannelScopeResponse>;
  listChannelMiners: (channelId: bigint, manufacturer?: string, model?: string) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint) => Promise<RolloutDevice[]>;
  onSave: (draft: ReleaseChannelDraft) => Promise<void>;
  onDelete?: (channel: ChannelView) => void;
  onApply: (channelId: bigint, assignments: AssignmentDraft[]) => Promise<void>;
}

// The per-channel management surface behind "Manage" (and the create flow):
// General, Applies to and Update behavior are saved together; Firmware is
// applied per model and starts an update paced by the saved behavior.
const ReleaseChannelManageView = ({
  channel,
  hasRefreshError = false,
  rollouts,
  firmwareFiles,
  minerNames,
  previewScope,
  listChannelMiners,
  listRolloutDevices,
  onSave,
  onDelete,
  onApply,
}: ReleaseChannelManageViewProps) => {
  // The draft is seeded once from the channel; the parent remounts this
  // view (keyed by channel id) when a different channel is opened.
  const [name, setName] = useState(channel?.name ?? "");
  const [description, setDescription] = useState(channel?.description ?? "");
  const [scope, setScope] = useState<ReleaseChannelScope>(() => channel?.scope ?? create(ReleaseChannelScopeSchema));
  const [behavior, setBehavior] = useState<RolloutBehavior>(() => channel?.behavior ?? defaultBehavior());
  const [preview, setPreview] = useState<PreviewReleaseChannelScopeResponse | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [savedDraft, setSavedDraft] = useState<ReleaseChannelDraft | null>(null);

  // A successful read restores the channel as the source of truth. Do not
  // carry an old save acknowledgement into a later, unrelated refresh error.
  if (!hasRefreshError && savedDraft !== null) setSavedDraft(null);

  // Staged (unapplied) firmware choices per pair key; absent key = server value.
  const [staged, setStaged] = useState<Record<string, string>>({});
  const [isApplying, setIsApplying] = useState(false);
  // Serialize settings and firmware writes, including clicks before React rerenders.
  const writeInFlightRef = useRef(false);
  const [showApplyDialog, setShowApplyDialog] = useState(false);
  // Pair whose miner table is open in the "View miners" modal.
  const [minersPair, setMinersPair] = useState<string | null>(null);

  const channelId = channel?.id;
  const previewForChannel = useCallback(
    (candidate: ReleaseChannelScope) => previewScope(candidate, channelId),
    [previewScope, channelId],
  );

  const savedSettings = hasRefreshError && savedDraft ? savedDraft : channel;
  const dirty =
    savedSettings === undefined ||
    name.trim() !== savedSettings.name ||
    description.trim() !== savedSettings.description ||
    !equals(ReleaseChannelScopeSchema, scope, savedSettings.scope ?? create(ReleaseChannelScopeSchema)) ||
    !equals(
      RolloutBehaviorSchema,
      behaviorForComparison(behavior),
      behaviorForComparison(savedSettings.behavior ?? create(RolloutBehaviorSchema)),
    );
  const hasConflicts = (preview?.conflicts.length ?? 0) > 0;
  // Preview totals cannot distinguish retained overlaps from new ones. The
  // server compares exact conflict relations when updating an existing channel.
  const isWriting = isSaving || isApplying;
  const canSave =
    dirty &&
    name.trim() !== "" &&
    !rolloutSizeError(behavior) &&
    (channel !== undefined || !hasConflicts) &&
    !isWriting;

  const handleSave = async () => {
    if (writeInFlightRef.current || !canSave) return;
    const submitted = { name: name.trim(), description: description.trim(), scope, behavior };
    writeInFlightRef.current = true;
    setIsSaving(true);
    try {
      await onSave(submitted);
      // Only these submitted values were saved; edits made while waiting
      // remain dirty even if the follow-up refresh fails.
      setSavedDraft(submitted);
      pushToast({
        message: channel ? "Release channel saved" : `Created release channel ${submitted.name}`,
        status: STATUSES.success,
      });
    } catch (error) {
      pushToast({
        message: error instanceof Error && error.message ? error.message : "Couldn't save the release channel",
        status: STATUSES.error,
      });
    } finally {
      writeInFlightRef.current = false;
      setIsSaving(false);
    }
  };

  const channelRollouts = channel ? rollouts.filter((r) => r.channelId === channel.id) : [];
  const activeByPair = new Map(channelRollouts.filter(isActive).map((r) => [pairKey(r), r]));
  const activeCount = activeByPair.size;
  const modelGroups = channel?.modelGroups ?? [];
  // Derived from the polled channel on every render so the open modal tracks
  // live firmware versions and phases; closes if the group empties.
  const minersGroup =
    minersPair !== null ? modelGroups.find((group) => observedPairKey(group) === minersPair) : undefined;

  const dirtyAssignmentsByPair = new Map<string, AssignmentDraft>();
  const invalidSelections = new Set<string>();
  for (const group of modelGroups) {
    const key = pairKey(group);
    const fileId = staged[key];
    if (fileId !== undefined && (fileId !== group.firmwareFileId || (fileId === "" && group.firmwareChecksum !== ""))) {
      const file = firmwareFiles.find((candidate) => candidate.id === fileId);
      const targetKey = minerTargetKey(group.manufacturer, group.model);
      const matchingFile =
        file && targetKey !== null && minerTargetKey(file.target_manufacturer, file.target_model) === targetKey
          ? file
          : undefined;
      if (fileId !== "" && !matchingFile) invalidSelections.add(key);
      // A catalog refresh must not retarget or collapse pending changes.
      // Keep invalid choices under the observed model until corrected; the
      // whole Apply remains blocked. Clears use the saved assignment's pair.
      const assignment = {
        manufacturer:
          fileId === "" ? group.firmwareTargetManufacturer : (matchingFile?.target_manufacturer ?? group.manufacturer),
        model: fileId === "" ? group.firmwareTargetModel : (matchingFile?.target_model ?? group.model),
        firmwareFileId: fileId,
      };
      // Several observed spellings can share one canonical assignment.
      dirtyAssignmentsByPair.set(key, assignment);
    }
  }
  const dirtyAssignments = [...dirtyAssignmentsByPair.values()];
  const exceedsAssignmentLimit = dirtyAssignments.length > MAX_FIRMWARE_ASSIGNMENTS;
  const canApply = dirtyAssignments.length > 0 && !exceedsAssignmentLimit && invalidSelections.size === 0 && !isWriting;

  // Human-readable version for a staged file id, for the dialog summary.
  const versionLabel = (fileId: string): string => {
    if (fileId === "") return "no firmware";
    const file = firmwareFiles.find((f) => f.id === fileId);
    return file?.firmware_version || file?.filename || "unknown version";
  };

  const handleApply = async () => {
    if (writeInFlightRef.current || !channel || !canApply) return;
    writeInFlightRef.current = true;
    setIsApplying(true);
    try {
      await onApply(channel.id, dirtyAssignments);
      setStaged({});
      setShowApplyDialog(false);
      pushToast({ message: "Firmware changes applied", status: STATUSES.success });
    } catch (error) {
      pushToast({
        message: error instanceof Error && error.message ? error.message : "Couldn't apply firmware changes",
        status: STATUSES.error,
      });
    } finally {
      writeInFlightRef.current = false;
      setIsApplying(false);
    }
  };

  const lastFinishedByPair = new Map<string, Rollout>();
  for (const r of channelRollouts) {
    if (r.status !== RolloutStatus.COMPLETED && r.status !== RolloutStatus.COMPLETED_WITH_FAILURES) continue;
    if (!lastFinishedByPair.has(pairKey(r))) lastFinishedByPair.set(pairKey(r), r); // rollouts arrive newest first
  }

  return (
    <div
      className="flex flex-col gap-8"
      data-testid={channel ? `release-channel-${channel.name}` : "release-channel-new"}
    >
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-3">
            <h2 className="text-heading-200 text-text-primary">{channel ? channel.name : "New release channel"}</h2>
            {activeCount > 0 ? <UpdateActivePill count={activeCount} testId="channel-update-pill" /> : null}
          </div>
          {channel ? (
            <div className="flex items-center gap-2 text-200 text-text-primary-50">
              <span>{channel.minerCount === 1 ? "1 miner" : `${channel.minerCount.toLocaleString()} miners`}</span>
              <span>·</span>
              <span>{modelGroups.length === 1 ? "1 model" : `${modelGroups.length} models`}</span>
            </div>
          ) : null}
        </div>
        <div className="flex gap-2 phone:flex-col phone:items-stretch">
          {channel && onDelete ? (
            <Button
              variant={variants.danger}
              size={sizes.compact}
              text="Delete"
              onClick={() => onDelete(channel)}
              testId="delete-channel"
            />
          ) : null}
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text={channel ? "Save changes" : "Create channel"}
            onClick={handleSave}
            disabled={!canSave}
            loading={isSaving}
            testId="save-channel"
          />
        </div>
      </div>

      <Section title="General">
        <div className="grid gap-3">
          <Input
            id="channel-name"
            label="Name"
            initValue={name}
            onChange={(value) => setName(value)}
            autoFocus={!channel}
          />
          <Textarea
            id="channel-description"
            label="Description"
            initValue={description}
            onChange={(value) => setDescription(value)}
          />
        </div>
      </Section>

      <Section
        title="Applies to"
        subtext="Miners are grouped by hardware model. Firmware is assigned per model below once the channel is saved."
      >
        <ScopeEditor
          scope={scope}
          onChange={setScope}
          previewScope={previewForChannel}
          onPreview={setPreview}
          editingExistingChannel={channel !== undefined}
        />
      </Section>

      <Section
        title="Update behavior"
        subtext="Batch and pilot sizes apply separately to each model. The offline limit is shared across the channel."
      >
        <RolloutControls behavior={behavior} onChange={setBehavior} />
      </Section>

      {channel ? (
        <Section
          title="Firmware"
          subtext="Assigned firmware is enforced: miners not on the assigned version are updated."
        >
          {modelGroups.length === 0 ? (
            <span className="text-300 text-text-primary-50">
              No miners in scope yet. Widen the selection above to group miners by model and assign firmware.
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
                {modelGroups.map((group) => {
                  const activeRollout = activeByPair.get(pairKey(group));
                  const counts = activeRollout ? rolloutDeviceCounts(activeRollout) : undefined;
                  return [
                    <tr
                      key={observedPairKey(group)}
                      className="border-t border-border-5"
                      data-testid={`model-group-${group.model}`}
                    >
                      <td className="py-3 pr-4 text-emphasis-300">{pairLabel(group)}</td>
                      <td className="py-3 pr-4">{group.minerCount.toLocaleString()}</td>
                      <td className="py-3 pr-4">
                        <FirmwarePickerCell
                          group={group}
                          firmwareFiles={firmwareFiles}
                          stagedFileId={staged[pairKey(group)]}
                          invalidSelection={invalidSelections.has(pairKey(group))}
                          onStageFirmware={(g, fileId) => setStaged((prev) => ({ ...prev, [pairKey(g)]: fileId }))}
                        />
                      </td>
                      <td className="py-3 pr-4">
                        <ModelStatusCell
                          group={group}
                          activeRollout={activeRollout}
                          lastFinished={lastFinishedByPair.get(pairKey(group))}
                        />
                      </td>
                      <td className="py-3 text-right">
                        {group.minerCount > 0 ? (
                          <Button
                            variant={variants.secondary}
                            size={sizes.compact}
                            text="View miners"
                            onClick={() => setMinersPair(observedPairKey(group))}
                            testId={`view-miners-${group.model}`}
                          />
                        ) : null}
                      </td>
                    </tr>,
                    activeRollout && counts ? (
                      <tr
                        key={`${observedPairKey(group)}-progress`}
                        data-testid={`model-group-rollout-progress-${group.model}`}
                      >
                        <td className="pb-3" colSpan={5}>
                          <div className="flex flex-col gap-1.5">
                            <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-200 text-text-primary-70">
                              <span>
                                {isPaused(activeRollout)
                                  ? `Updating to ${activeRollout.firmwareVersion} (paused)`
                                  : `Updating to ${activeRollout.firmwareVersion}`}
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

          {dirtyAssignments.length > 0 ? (
            <div className="flex items-center justify-between gap-4 rounded-lg bg-intent-warning-10 px-4 py-3">
              <div className="grid gap-1 text-300 text-text-primary">
                <span>
                  {dirtyAssignments.length === 1
                    ? "1 firmware change pending"
                    : `${dirtyAssignments.length} firmware changes pending`}
                  {" — applying starts an update per model."}
                </span>
                {exceedsAssignmentLimit ? <p role="alert">{assignmentLimitMessage}</p> : null}
              </div>
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
                  disabled={!canApply}
                  onClick={() => {
                    if (!writeInFlightRef.current && canApply) setShowApplyDialog(true);
                  }}
                  testId="apply-firmware-changes"
                />
              </div>
            </div>
          ) : null}
        </Section>
      ) : null}

      {channel && minersGroup ? (
        <ModelMinersModal
          channelId={channel.id}
          channelName={channel.name}
          group={minersGroup}
          activeRollout={activeByPair.get(pairKey(minersGroup))}
          minerNames={minerNames}
          listChannelMiners={listChannelMiners}
          listRolloutDevices={listRolloutDevices}
          onClose={() => setMinersPair(null)}
        />
      ) : null}

      {channel ? (
        <Dialog
          open={showApplyDialog}
          title="Start firmware update?"
          subtitle={`One update starts per changed model in ${channel.name}. Pacing: ${pacingSummary(savedSettings?.behavior).toLowerCase()}.`}
          testId="apply-firmware-dialog"
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
              text: "Start update",
              variant: variants.primary,
              onClick: handleApply,
              disabled: !canApply,
              loading: isApplying,
            },
          ]}
        >
          {invalidSelections.size > 0 ? (
            <p role="alert" className="mb-3 text-200 text-intent-critical-fill">
              Choose valid firmware for every changed model or discard the pending changes.
            </p>
          ) : null}
          {exceedsAssignmentLimit ? (
            <p role="alert" className="mb-3 text-200 text-text-primary">
              {assignmentLimitMessage}
            </p>
          ) : null}
          <div>
            {dirtyAssignments.map((assignment) => (
              <div
                key={pairKey(assignment)}
                className="flex items-baseline justify-between gap-4 border-t border-border-5 py-2.5 text-200"
              >
                <span className="text-text-primary">{pairLabel(assignment)}</span>
                <span className="text-right text-text-primary-70">{versionLabel(assignment.firmwareFileId)}</span>
              </div>
            ))}
          </div>
          {dirty ? (
            <p className="mt-3 text-200 text-text-primary-70">
              Unsaved channel changes are not applied to this update. Save the channel first to use them.
            </p>
          ) : null}
        </Dialog>
      ) : null}
    </div>
  );
};

export default ReleaseChannelManageView;
