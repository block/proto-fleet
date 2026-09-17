import { type ReactElement, type ReactNode, useCallback, useMemo, useRef, useState } from "react";
import { create, equals } from "@bufbuild/protobuf";

import { defaultBehavior, rolloutBehaviorErrors } from "./behaviorUtils";
import { ModelStatusCell } from "./channelStatus";
import FirmwarePickerButton from "./FirmwarePickerButton";
import ModelMinersModal from "./ModelMinersModal";
import RolloutControls from "./RolloutControls";
import {
  activeRolloutForGroup,
  assignmentKey,
  hasUnavailableAssignedFirmware,
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
import { isScopeEmpty, rebaseScope, scopeSelectionsEqual, scopeValidationErrors } from "./scopeUtils";
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
  RolloutMethod,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { AssignmentDraft, ChannelView, ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";
import {
  minerTargetKey,
  trimMinerTarget,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar from "@/shared/components/CompositionBar";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Textarea from "@/shared/components/Textarea";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const MAX_FIRMWARE_ASSIGNMENTS = 100;
const assignmentLimitMessage =
  "Apply up to 100 model changes at a time. Revert some selections or discard them and choose fewer models.";
const delegatedApplyMessage =
  "Firmware updates for externally controlled channels are not available yet. Choose and save another update method before applying firmware.";
const unsupportedTargetMessage =
  "Release channels require manufacturer and model names of 1–255 printable ASCII characters. Correct the miner's reported identity before assigning firmware.";

// FirmwareAssignment is narrower than observed miner and upload metadata.
// Validate the trimmed values sent to the API without changing identity joins.
const supportsFirmwareAssignment = (manufacturer: string | undefined, model: string | undefined): boolean =>
  [manufacturer, model].every((value) => {
    const target = trimMinerTarget(value ?? "");
    return target.length <= 255 && /^[!-~](?:[ -~]*[!-~])?$/.test(target);
  });

const supportsFirmwareFileAssignment = (file: FirmwareFileInfo): boolean => {
  // ResolveFirmwareArtifact requires a version even for legacy catalog files.
  // Match its Go whitespace trimming and Unicode code-point limit.
  const version = trimMinerTarget(file.firmware_version ?? "");
  return (
    supportsFirmwareAssignment(file.target_manufacturer, file.target_model) &&
    version !== "" &&
    !version.includes("\u0000") &&
    Array.from(version).length <= 255
  );
};

interface AcknowledgedAssignment extends AssignmentDraft {
  label: string;
}

function channelTextError(value: string, label: string, maxLength: number): string | undefined {
  const submitted = value.trim();
  if (submitted.includes("\u0000")) return `${label} cannot contain null characters.`;
  // Protobuf string limits count Unicode code points, not UTF-16 code units.
  if (Array.from(submitted).length > maxLength) return `${label} must be ${maxLength} characters or fewer.`;
  return undefined;
}

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

// Carry remote changes into untouched fields without overwriting local edits,
// including inactive values that request normalization intentionally omits.
function rebaseBehavior(draft: RolloutBehavior, previous: RolloutBehavior, incoming: RolloutBehavior): RolloutBehavior {
  if (equals(RolloutBehaviorSchema, previous, incoming)) return draft;
  const keepLocal = <T,>(local: T, base: T, remote: T): T => (Object.is(local, base) ? remote : local);
  const thresholds = create(RolloutAutomationThresholdsSchema);
  for (const field of [
    "maxHashrateDropPercent",
    "maxEfficiencyIncreasePercent",
    "maxTemperatureIncreaseCelsius",
    "maxNewErrors",
    "minSampleCoveragePercent",
  ] as const) {
    thresholds[field] = keepLocal(
      draft.thresholds?.[field],
      previous.thresholds?.[field],
      incoming.thresholds?.[field],
    );
  }
  return create(RolloutBehaviorSchema, {
    method: keepLocal(draft.method, previous.method, incoming.method),
    order: keepLocal(draft.order, previous.order, incoming.order),
    batchSize: keepLocal(draft.batchSize, previous.batchSize, incoming.batchSize),
    pilotSize: keepLocal(draft.pilotSize, previous.pilotSize, incoming.pilotSize),
    waitBetweenBatchesSeconds: keepLocal(
      draft.waitBetweenBatchesSeconds,
      previous.waitBetweenBatchesSeconds,
      incoming.waitBetweenBatchesSeconds,
    ),
    reviewAfterEachBatch: keepLocal(
      draft.reviewAfterEachBatch,
      previous.reviewAfterEachBatch,
      incoming.reviewAfterEachBatch,
    ),
    autoContinueOnHealthyTelemetry: keepLocal(
      draft.autoContinueOnHealthyTelemetry,
      previous.autoContinueOnHealthyTelemetry,
      incoming.autoContinueOnHealthyTelemetry,
    ),
    stabilizationSeconds: keepLocal(
      draft.stabilizationSeconds,
      previous.stabilizationSeconds,
      incoming.stabilizationSeconds,
    ),
    maxConcurrentOffline: keepLocal(
      draft.maxConcurrentOffline,
      previous.maxConcurrentOffline,
      incoming.maxConcurrentOffline,
    ),
    controllerTimeoutSeconds: keepLocal(
      draft.controllerTimeoutSeconds,
      previous.controllerTimeoutSeconds,
      incoming.controllerTimeoutSeconds,
    ),
    thresholds,
  });
}

interface FirmwarePickerCellProps {
  group: ReleaseChannelModelGroup;
  firmwareFiles: FirmwareFileInfo[];
  stagedFileId: string | undefined;
  acknowledgedAssignment?: AcknowledgedAssignment;
  selectionError?: string;
  onStageFirmware: (group: ReleaseChannelModelGroup, fileId: string) => void;
}

// Files whose target pair matches the group's observed pair under the
// server's ASCII fold; a group with an unknown identity matches nothing.
const filesForGroup = (firmwareFiles: FirmwareFileInfo[], group: ReleaseChannelModelGroup): FirmwareFileInfo[] => {
  const key = minerTargetKey(group.manufacturer, group.model);
  return key === null || !supportsFirmwareAssignment(group.manufacturer, group.model)
    ? []
    : firmwareFiles.filter(
        (f) => supportsFirmwareFileAssignment(f) && minerTargetKey(f.target_manufacturer, f.target_model) === key,
      );
};

const FirmwarePickerCell = ({
  group,
  firmwareFiles,
  stagedFileId,
  acknowledgedAssignment,
  selectionError,
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
  const value =
    stagedFileId ??
    acknowledgedAssignment?.firmwareFileId ??
    (group.firmwareFileId || (group.firmwareChecksum ? null : ""));
  const assignment = acknowledgedAssignment
    ? { value: acknowledgedAssignment.firmwareFileId, label: acknowledgedAssignment.label }
    : group.firmwareChecksum
      ? { value: group.firmwareFileId || null, label: group.firmwareVersion }
      : undefined;
  const targetError = supportsFirmwareAssignment(group.manufacturer, group.model)
    ? undefined
    : unsupportedTargetMessage;

  return (
    <div className="grid gap-1">
      <FirmwarePickerButton
        label={`Firmware for ${pairLabel(group)}`}
        options={options}
        value={value}
        assignment={assignment}
        onChange={(value) => onStageFirmware(group, value)}
        testId={`channel-firmware-select-${group.model}`}
      />
      {!acknowledgedAssignment && hasUnavailableAssignedFirmware(group) ? (
        <p role="alert" className="text-200 text-intent-critical-fill">
          Assigned firmware is unavailable. Updates waiting for this file cannot proceed. Restore the file with the same
          checksum, or apply another version or “No firmware”.
        </p>
      ) : null}
      {selectionError || targetError ? (
        <p role="alert" className="text-200 text-intent-critical-fill">
          {selectionError || targetError}
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
  listChannelMiners: (
    channelId: bigint,
    manufacturer?: string,
    model?: string,
    signal?: AbortSignal,
  ) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint, signal?: AbortSignal) => Promise<RolloutDevice[]>;
  onSave: (draft: ReleaseChannelDraft) => Promise<void>;
  onDelete?: (channel: ChannelView) => void;
  onApply: (channelId: bigint, assignments: AssignmentDraft[]) => Promise<Rollout[] | void>;
  writeLock?: { isLocked: boolean; tryAcquire: () => boolean; release: () => void };
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
  writeLock,
}: ReleaseChannelManageViewProps) => {
  // A different channel remounts this view; later polls rebase only fields
  // that still match the previous authoritative settings for this channel.
  const [name, setName] = useState(channel?.name ?? "");
  const [description, setDescription] = useState(channel?.description ?? "");
  const [scope, setScope] = useState<ReleaseChannelScope>(() => channel?.scope ?? create(ReleaseChannelScopeSchema));
  const [behavior, setBehavior] = useState<RolloutBehavior>(() => channel?.behavior ?? defaultBehavior());
  const [preview, setPreview] = useState<{
    scope: ReleaseChannelScope;
    load: (scope: ReleaseChannelScope) => Promise<PreviewReleaseChannelScopeResponse>;
    value: PreviewReleaseChannelScopeResponse;
  } | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [savedDraft, setSavedDraft] = useState<{
    settings: ReleaseChannelDraft;
    beforeSave: ChannelView | undefined;
  } | null>(null);
  const [settingsBase, setSettingsBase] = useState<ChannelView | ReleaseChannelDraft | undefined>(channel);

  // A successful read restores the channel as the source of truth. Do not
  // carry an old save acknowledgement into a later, unrelated refresh error.
  if (!hasRefreshError && savedDraft !== null && channel !== savedDraft.beforeSave) setSavedDraft(null);

  // Staged (unapplied) firmware choices per pair key; absent key = server value.
  const [staged, setStaged] = useState<Record<string, string>>({});
  const [appliedAssignments, setAppliedAssignments] = useState<{
    assignments: Record<string, AcknowledgedAssignment>;
    beforeApply: ChannelView;
  } | null>(null);
  if (!hasRefreshError && appliedAssignments && channel !== appliedAssignments.beforeApply) setAppliedAssignments(null);
  const acknowledgedAssignments =
    appliedAssignments && (hasRefreshError || channel === appliedAssignments.beforeApply)
      ? appliedAssignments.assignments
      : {};
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
  const handlePreview = useCallback(
    (value: PreviewReleaseChannelScopeResponse | null) =>
      setPreview(value ? { scope, load: previewForChannel, value } : null),
    [scope, previewForChannel],
  );

  const savedSettings =
    savedDraft && (hasRefreshError || channel === savedDraft.beforeSave) ? savedDraft.settings : channel;
  if (!isSaving && savedSettings !== settingsBase) {
    setSettingsBase(savedSettings);
    if (settingsBase && savedSettings) {
      if (name.trim() === settingsBase.name && savedSettings.name !== settingsBase.name) setName(savedSettings.name);
      if (description.trim() === settingsBase.description && savedSettings.description !== settingsBase.description) {
        setDescription(savedSettings.description);
      }
      setScope(
        rebaseScope(
          scope,
          settingsBase.scope ?? create(ReleaseChannelScopeSchema),
          savedSettings.scope ?? create(ReleaseChannelScopeSchema),
        ),
      );
      setBehavior(
        rebaseBehavior(
          behavior,
          settingsBase.behavior ?? create(RolloutBehaviorSchema),
          savedSettings.behavior ?? create(RolloutBehaviorSchema),
        ),
      );
    }
  }
  const dirty =
    savedSettings === undefined ||
    name.trim() !== savedSettings.name ||
    description.trim() !== savedSettings.description ||
    !scopeSelectionsEqual(scope, savedSettings.scope ?? create(ReleaseChannelScopeSchema)) ||
    !equals(
      RolloutBehaviorSchema,
      behaviorForComparison(behavior),
      behaviorForComparison(savedSettings.behavior ?? create(RolloutBehaviorSchema)),
    );
  const currentPreview = preview?.scope === scope && preview.load === previewForChannel ? preview.value : null;
  const canCreateInScope =
    isScopeEmpty(scope) ||
    (currentPreview !== null && currentPreview.conflictCount === 0 && currentPreview.conflicts.length === 0);
  // Preview totals cannot distinguish retained overlaps from new ones. The
  // server compares exact conflict relations when updating an existing channel.
  const isWriting = isSaving || isApplying || (writeLock?.isLocked ?? false);
  const nameError = name.trim() === "" ? "Enter a name." : channelTextError(name, "Name", 100);
  const descriptionError = channelTextError(description, "Description", 1000);
  const isDelegated = behavior.method === RolloutMethod.DELEGATED;
  const canSave =
    dirty &&
    !isDelegated &&
    !nameError &&
    !descriptionError &&
    scopeValidationErrors(scope).length === 0 &&
    Object.keys(rolloutBehaviorErrors(behavior)).length === 0 &&
    (channel !== undefined || canCreateInScope) &&
    !isWriting;

  const handleSave = async () => {
    if (writeInFlightRef.current || !canSave) return;
    if (writeLock && !writeLock.tryAcquire()) return;
    const submitted = {
      name: name.trim(),
      description: description.trim(),
      scope,
      behavior: rolloutBehaviorForRequest(behavior),
    };
    writeInFlightRef.current = true;
    setIsSaving(true);
    try {
      await onSave(submitted);
      // Only these submitted values were saved; edits made while waiting
      // remain dirty even if the follow-up refresh fails.
      setSavedDraft({ settings: submitted, beforeSave: channel });
      setSettingsBase(submitted);
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
      writeLock?.release();
    }
  };

  const channelRollouts = channel ? rollouts.filter((r) => r.channelId === channel.id) : [];
  const rolloutsById = new Map(channelRollouts.map((rollout) => [rollout.id, rollout]));
  const activeForGroup = (group: ReleaseChannelModelGroup) =>
    activeRolloutForGroup(channelId ?? 0n, group, rolloutsById);
  const modelGroups = channel?.modelGroups ?? [];
  const activeCount = new Set(modelGroups.map((group) => activeForGroup(group)?.id).filter((id) => id !== undefined))
    .size;
  // Derived from the polled channel on every render so the open modal tracks
  // live firmware versions and phases; closes if the group empties.
  const minersGroup =
    minersPair !== null ? modelGroups.find((group) => observedPairKey(group) === minersPair) : undefined;
  const minersRollout = minersGroup ? activeForGroup(minersGroup) : undefined;
  const minersRolloutPending = minersGroup && minersGroup.activeRolloutId > 0n && !minersRollout;

  const dirtyAssignmentsByPair = new Map<string, AssignmentDraft>();
  const invalidSelections = new Map<string, string>();
  for (const group of modelGroups) {
    const key = pairKey(group);
    const fileId = staged[key];
    const acknowledged = acknowledgedAssignments[key];
    const savedFileId = acknowledged?.firmwareFileId ?? group.firmwareFileId;
    const hasAssignment = acknowledged ? acknowledged.firmwareFileId !== "" : group.firmwareChecksum !== "";
    if (fileId !== undefined && (fileId !== savedFileId || (fileId === "" && hasAssignment))) {
      const file = firmwareFiles.find((candidate) => candidate.id === fileId);
      const targetKey = minerTargetKey(group.manufacturer, group.model);
      const matchingFile =
        file &&
        supportsFirmwareFileAssignment(file) &&
        supportsFirmwareAssignment(group.manufacturer, group.model) &&
        targetKey !== null &&
        minerTargetKey(file.target_manufacturer, file.target_model) === targetKey
          ? file
          : undefined;
      if (fileId !== "" && !matchingFile) {
        invalidSelections.set(
          key,
          "Selected firmware is unavailable for this model. Choose another version or discard the pending changes.",
        );
      }
      // A catalog refresh must not retarget or collapse pending changes.
      // Keep invalid choices under the observed model until corrected; the
      // whole Apply remains blocked. Clears use the saved assignment's pair.
      const assignment = {
        manufacturer:
          fileId === ""
            ? (acknowledged?.manufacturer ?? group.firmwareTargetManufacturer)
            : (matchingFile?.target_manufacturer ?? group.manufacturer),
        model:
          fileId === ""
            ? (acknowledged?.model ?? group.firmwareTargetModel)
            : (matchingFile?.target_model ?? group.model),
        firmwareFileId: fileId,
      };
      if (!supportsFirmwareAssignment(assignment.manufacturer, assignment.model)) {
        invalidSelections.set(
          key,
          "This firmware change requires manufacturer and model names of 1–255 printable ASCII characters. Correct the target identity or discard the pending changes.",
        );
      }
      // Several observed spellings can share one canonical assignment.
      dirtyAssignmentsByPair.set(key, {
        ...assignment,
        manufacturer: trimMinerTarget(assignment.manufacturer),
        model: trimMinerTarget(assignment.model),
      });
    }
  }
  const dirtyAssignments = [...dirtyAssignmentsByPair.values()];
  const assignmentCount = dirtyAssignments.filter((assignment) => assignment.firmwareFileId !== "").length;
  const clearCount = dirtyAssignments.length - assignmentCount;
  const exceedsAssignmentLimit = dirtyAssignments.length > MAX_FIRMWARE_ASSIGNMENTS;
  const savedMethodDelegated = savedSettings?.behavior?.method === RolloutMethod.DELEGATED;
  const delegatedApplyBlocked = savedMethodDelegated && assignmentCount > 0;
  const canApply =
    dirtyAssignments.length > 0 &&
    !delegatedApplyBlocked &&
    !exceedsAssignmentLimit &&
    invalidSelections.size === 0 &&
    !isWriting;

  const applyTitle =
    clearCount === 0
      ? "Start firmware update?"
      : assignmentCount === 0
        ? "Clear firmware assignments?"
        : "Apply firmware changes?";
  const applyButtonText =
    clearCount === 0 ? "Start update" : assignmentCount === 0 ? "Clear assignments" : "Apply changes";
  const applySummary = [
    assignmentCount > 0
      ? `Assign firmware for ${assignmentCount} ${assignmentCount === 1 ? "model" : "models"} in ${channel?.name}. Updates start where needed. Pacing: ${pacingSummary(savedSettings?.behavior).toLowerCase()}.`
      : "",
    clearCount > 0
      ? `Clear firmware assignments for ${clearCount} ${clearCount === 1 ? "model" : "models"} in ${channel?.name}. Clearing stops enforcement and cancels remaining updates for these models; updates already dispatched may finish.`
      : "",
  ]
    .filter(Boolean)
    .join(" ");

  // Human-readable version for a staged file id, for the dialog summary.
  const versionLabel = (fileId: string): string => {
    if (fileId === "") return "no firmware";
    const file = firmwareFiles.find((f) => f.id === fileId);
    return file?.firmware_version || file?.filename || "unknown version";
  };

  const handleApply = async () => {
    if (writeInFlightRef.current || !channel || !canApply) return;
    if (writeLock && !writeLock.tryAcquire()) return;
    const submitted = dirtyAssignments.map((assignment) => ({
      ...assignment,
      label: versionLabel(assignment.firmwareFileId),
    }));
    writeInFlightRef.current = true;
    setIsApplying(true);
    try {
      const startedRollouts = await onApply(channel.id, dirtyAssignments);
      const acknowledged: Record<string, AcknowledgedAssignment> = {};
      for (const assignment of submitted) {
        const rollout = startedRollouts?.find((candidate) => pairKey(candidate) === pairKey(assignment));
        acknowledged[pairKey(assignment)] = {
          ...assignment,
          label: rollout?.firmwareVersion || `${assignment.label} (details pending)`,
        };
      }
      // This acknowledges the write, not live miner convergence. Keep the
      // polled checksum/generation/counts untouched until a fresh read arrives.
      setAppliedAssignments((current) => ({
        assignments: { ...current?.assignments, ...acknowledged },
        beforeApply: channel,
      }));
      setStaged((current) => {
        const remaining = { ...current };
        for (const assignment of submitted) {
          const key = pairKey(assignment);
          if (remaining[key] === assignment.firmwareFileId) delete remaining[key];
        }
        return remaining;
      });
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
      writeLock?.release();
    }
  };

  const lastFinishedByPair = new Map<string, Rollout>();
  for (const r of channelRollouts) {
    if (r.status !== RolloutStatus.COMPLETED && r.status !== RolloutStatus.COMPLETED_WITH_FAILURES) continue;
    if (!lastFinishedByPair.has(assignmentKey(r))) lastFinishedByPair.set(assignmentKey(r), r); // rollouts arrive newest first
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
              disabled={isWriting}
              onClick={() => {
                if (!writeInFlightRef.current && !isWriting) onDelete(channel);
              }}
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
            error={nameError}
            autoFocus={!channel}
          />
          <Textarea
            id="channel-description"
            label="Description"
            initValue={description}
            onChange={(value) => setDescription(value)}
            error={descriptionError}
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
          onPreview={handlePreview}
          editingExistingChannel={channel !== undefined}
        />
      </Section>

      <Section
        title="Update behavior"
        subtext="Batch and pilot sizes apply separately to each model. The offline limit is shared across the channel."
      >
        {isDelegated ? (
          <p role="alert" className="text-200 text-intent-critical-fill" data-testid="delegated-save-unavailable">
            Saving externally controlled channels is not available yet. Choose another update method to save changes.
          </p>
        ) : null}
        <RolloutControls
          behavior={behavior}
          onChange={setBehavior}
          allowDelegated={savedSettings?.behavior?.method === RolloutMethod.DELEGATED}
        />
      </Section>

      {channel ? (
        <Section
          title="Firmware"
          subtext="Miners not on the assigned version are updated when the assigned firmware file is available."
        >
          {delegatedApplyBlocked ? (
            <p role="alert" className="text-200 text-intent-critical-fill" data-testid="delegated-apply-unavailable">
              {delegatedApplyMessage}
            </p>
          ) : null}
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
                  const acknowledged = acknowledgedAssignments[pairKey(group)];
                  const activeRollout = acknowledged ? undefined : activeForGroup(group);
                  const rolloutPending = group.activeRolloutId > 0n && !activeRollout;
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
                          acknowledgedAssignment={acknowledged}
                          selectionError={invalidSelections.get(pairKey(group))}
                          onStageFirmware={(g, fileId) => setStaged((prev) => ({ ...prev, [pairKey(g)]: fileId }))}
                        />
                      </td>
                      <td className="py-3 pr-4">
                        {acknowledged ? (
                          <span className="text-text-primary-50">Refreshing update status</span>
                        ) : (
                          <ModelStatusCell
                            group={group}
                            activeRollout={activeRollout}
                            lastFinished={lastFinishedByPair.get(assignmentKey(group))}
                          />
                        )}
                      </td>
                      <td className="py-3 text-right">
                        {group.minerCount > 0 ? (
                          <Button
                            variant={variants.secondary}
                            size={sizes.compact}
                            text="View miners"
                            disabled={acknowledged !== undefined || rolloutPending}
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

      {channel && minersGroup && !minersRolloutPending && !acknowledgedAssignments[pairKey(minersGroup)] ? (
        <ModelMinersModal
          channelId={channel.id}
          channelName={channel.name}
          group={minersGroup}
          activeRollout={minersRollout}
          minerNames={minerNames}
          listChannelMiners={listChannelMiners}
          listRolloutDevices={listRolloutDevices}
          onClose={() => setMinersPair(null)}
        />
      ) : null}

      {channel ? (
        <Dialog
          open={showApplyDialog}
          title={applyTitle}
          subtitle={applySummary}
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
              text: applyButtonText,
              variant: variants.primary,
              onClick: handleApply,
              disabled: !canApply,
              loading: isApplying,
            },
          ]}
        >
          {delegatedApplyBlocked ? (
            <p role="alert" className="mb-3 text-200 text-intent-critical-fill">
              {delegatedApplyMessage}
            </p>
          ) : null}
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
              Unsaved channel changes are not applied with these firmware changes. Save the channel first to use them.
            </p>
          ) : null}
        </Dialog>
      ) : null}
    </div>
  );
};

export default ReleaseChannelManageView;
