import {
  type ReactElement,
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { create, equals } from "@bufbuild/protobuf";

import { behaviorForComparison, defaultBehavior, rebaseBehavior, rolloutBehaviorErrors } from "./behaviorUtils";
import ChangePreviewTable from "./ChangePreviewTable";
import ChannelSettingsChanges from "./ChannelSettingsChanges";
import { getChannelSettingsChanges } from "./channelSettingsChangesUtils";
import { ModelStatusCell } from "./channelStatus";
import FirmwarePickerButton from "./FirmwarePickerButton";
import ModelMinersModal from "./ModelMinersModal";
import RolloutControls, { type RolloutNumberDraft } from "./RolloutControls";
import RolloutProgressIndicator from "./RolloutProgressIndicator";
import {
  activeRolloutForGroup,
  channelAssignmentKey,
  hasUnavailableAssignedFirmware,
  lastFinishedByChannelAssignment,
  pacingSummary,
  pairKey,
  pairLabel,
} from "./rolloutStatus";
import ScopeEditor from "./ScopeEditor";
import { isScopeEmpty, rebaseScope, scopeSelectionsEqual, scopeValidationErrors } from "./scopeUtils";
import type { ChannelHistoryState } from "./useChannelHistory";
import {
  type PreviewReleaseChannelScopeResponse,
  type ReleaseChannelMiner,
  ReleaseChannelModelGroupSchema,
  type ReleaseChannelScope,
  ReleaseChannelScopeSchema,
  type Rollout,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  type RolloutDevice,
  RolloutMethod,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { rolloutBehaviorForRequest } from "@/protoFleet/api/rolloutBehavior";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type {
  AssignmentDraft,
  ChannelView,
  ReleaseChannelDraft,
  ChannelModelGroupView as ReleaseChannelModelGroup,
} from "@/protoFleet/api/useReleaseChannels";
import {
  minerTargetKey,
  trimMinerTarget,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Textarea from "@/shared/components/Textarea";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const MAX_FIRMWARE_ASSIGNMENTS = 100;
const assignmentLimitMessage =
  "Apply up to 100 model changes at a time. Revert some selections or discard them and choose fewer models.";
const delegatedApplyMessage =
  "Firmware updates for externally controlled channels are not available yet. Choose another update method in Channel settings before applying changes.";
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
  const submitted = trimMinerTarget(value);
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

interface FirmwarePickerCellProps {
  group: ReleaseChannelModelGroup;
  firmwareFiles: FirmwareFileInfo[];
  stagedFileId: string | undefined;
  acknowledgedAssignment?: AcknowledgedAssignment;
  selectionError?: string;
  disabled?: boolean;
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
  disabled,
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
  if (group.rollbackPending) {
    return (
      <p role="status" className="text-200 text-text-primary-70">
        Rollback saved. Refreshing firmware assignment…
      </p>
    );
  }
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
        disabled={disabled}
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
  historyState?: ChannelHistoryState;
  firmwareFiles: FirmwareFileInfo[];
  minerNames: Record<string, string>;
  previewScope: (
    scope: ReleaseChannelScope,
    channelId?: bigint,
    signal?: AbortSignal,
  ) => Promise<PreviewReleaseChannelScopeResponse>;
  listChannelMiners: (
    channelId: bigint,
    manufacturer?: string,
    model?: string,
    signal?: AbortSignal,
  ) => Promise<ReleaseChannelMiner[]>;
  listRolloutDevices: (rolloutId: bigint, signal?: AbortSignal) => Promise<RolloutDevice[]>;
  onSave: (draft: ReleaseChannelDraft) => Promise<void>;
  onCancelCreate?: () => void;
  onDelete?: (channel: ChannelView) => void;
  onShowHistory?: (channel: ChannelView) => void;
  onDirtyChange?: (dirty: boolean) => void;
  onApply: (channelId: bigint, assignments: AssignmentDraft[]) => Promise<Rollout[] | void>;
  writeLock?: { isLocked: boolean; tryAcquire: () => boolean; release: () => void };
}

// The per-channel management surface behind "Manage" (and the create flow):
// Assigned miners and firmware are the main management surface. General,
// Applies to and Update behavior share a draft with firmware assignments.
// Apply saves settings first so new updates use the reviewed behavior and scope.
const ReleaseChannelManageView = ({
  channel,
  hasRefreshError = false,
  rollouts,
  historyState,
  firmwareFiles,
  minerNames,
  previewScope,
  listChannelMiners,
  listRolloutDevices,
  onSave,
  onCancelCreate,
  onDelete,
  onShowHistory,
  onDirtyChange,
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
    load: (scope: ReleaseChannelScope, signal?: AbortSignal) => Promise<PreviewReleaseChannelScopeResponse>;
    value: PreviewReleaseChannelScopeResponse;
  } | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [isApplying, setIsApplying] = useState(false);
  const latestChannelRef = useRef(channel);
  latestChannelRef.current = channel;
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const [showSettings, setShowSettings] = useState(false);
  const [numberDraft, setNumberDraft] = useState<RolloutNumberDraft>({});
  const [savedDraft, setSavedDraft] = useState<{
    settings: ReleaseChannelDraft;
    beforeSave: ChannelView | undefined;
  } | null>(null);
  const [settingsBase, setSettingsBase] = useState<ChannelView | ReleaseChannelDraft | undefined>(channel);

  // A successful read restores the channel as the source of truth. Do not
  // carry an old save acknowledgement into a later, unrelated refresh error.
  if (!isSaving && !isApplying && !hasRefreshError && savedDraft !== null && channel !== savedDraft.beforeSave)
    setSavedDraft(null);

  // Staged (unapplied) firmware choices per pair key; absent key = server value.
  // Retain the observed pair as well: saving a narrower scope can remove an
  // unassigned group before the firmware write succeeds or can be retried.
  const [staged, setStaged] = useState<Record<string, { fileId: string; group: ReleaseChannelModelGroup }>>({});
  const [appliedAssignments, setAppliedAssignments] = useState<{
    assignments: Record<string, AcknowledgedAssignment>;
    beforeApply: ChannelView;
  } | null>(null);
  if (!hasRefreshError && appliedAssignments && channel !== appliedAssignments.beforeApply) setAppliedAssignments(null);
  const acknowledgedAssignments =
    appliedAssignments && (hasRefreshError || channel === appliedAssignments.beforeApply)
      ? appliedAssignments.assignments
      : {};
  const [applyError, setApplyError] = useState<string | null>(null);
  // Serialize settings and firmware writes, including clicks before React rerenders.
  const writeInFlightRef = useRef(false);
  const [showApplyDialog, setShowApplyDialog] = useState(false);
  // Pair whose miner table is open in the "View miners" modal.
  const [minersPair, setMinersPair] = useState<string | null>(null);

  const channelId = channel?.id;
  const previewForChannel = useCallback(
    (candidate: ReleaseChannelScope, signal?: AbortSignal) => previewScope(candidate, channelId, signal),
    [previewScope, channelId],
  );
  const handlePreview = useCallback(
    (value: PreviewReleaseChannelScopeResponse | null) =>
      setPreview(value ? { scope, load: previewForChannel, value } : null),
    [scope, previewForChannel],
  );

  const savedSettings =
    savedDraft && (isApplying || hasRefreshError || channel === savedDraft.beforeSave) ? savedDraft.settings : channel;
  if (!isSaving && !isApplying && savedSettings !== settingsBase) {
    setSettingsBase(savedSettings);
    if (settingsBase && savedSettings) {
      if (trimMinerTarget(name) === settingsBase.name && savedSettings.name !== settingsBase.name)
        setName(savedSettings.name);
      if (
        trimMinerTarget(description) === settingsBase.description &&
        savedSettings.description !== settingsBase.description
      ) {
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
    trimMinerTarget(name) !== savedSettings.name ||
    trimMinerTarget(description) !== savedSettings.description ||
    !scopeSelectionsEqual(scope, savedSettings.scope ?? create(ReleaseChannelScopeSchema)) ||
    !equals(
      RolloutBehaviorSchema,
      behaviorForComparison(behavior),
      behaviorForComparison(savedSettings.behavior ?? create(RolloutBehaviorSchema)),
    );
  const currentPreview = preview?.scope === scope && preview.load === previewForChannel ? preview.value : null;
  const scopeChanged =
    channel !== undefined && !scopeSelectionsEqual(scope, savedSettings?.scope ?? create(ReleaseChannelScopeSchema));
  const behaviorChanged = !equals(
    RolloutBehaviorSchema,
    behaviorForComparison(behavior),
    behaviorForComparison(savedSettings?.behavior ?? create(RolloutBehaviorSchema)),
  );
  const canCreateInScope =
    isScopeEmpty(scope) ||
    (currentPreview !== null && currentPreview.conflictCount === 0 && currentPreview.conflicts.length === 0);
  // Preview totals cannot distinguish retained overlaps from new ones. The
  // server compares exact conflict relations when updating an existing channel.
  const isWriting = isSaving || isApplying || (writeLock?.isLocked ?? false);
  const nameError = trimMinerTarget(name) === "" ? "Enter a name." : channelTextError(name, "Name", 100);
  const descriptionError = channelTextError(description, "Description", 1000);
  const isDelegated = behavior.method === RolloutMethod.DELEGATED;
  const settingsValid =
    !isDelegated &&
    !nameError &&
    !descriptionError &&
    scopeValidationErrors(scope).length === 0 &&
    Object.keys(rolloutBehaviorErrors(behavior)).length === 0 &&
    (channel !== undefined || canCreateInScope);
  const canSave = dirty && settingsValid && !isWriting;
  const settingsDraft: ReleaseChannelDraft = {
    name: trimMinerTarget(name),
    description: trimMinerTarget(description),
    scope,
    behavior: rolloutBehaviorForRequest(behavior),
  };

  const handleSave = async () => {
    if (writeInFlightRef.current || !canSave) return;
    if (writeLock && !writeLock.tryAcquire()) return;
    const submitted = {
      name: trimMinerTarget(name),
      description: trimMinerTarget(description),
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
  const liveGroups = channel?.modelGroups ?? [];
  const livePairs = new Set(liveGroups.map(pairKey));
  const scopeModels = scopeChanged ? (currentPreview?.models ?? []) : [];
  const addedGroups = scopeModels
    .filter((model) => !livePairs.has(pairKey(model)))
    .map((model) =>
      create(ReleaseChannelModelGroupSchema, {
        manufacturer: model.manufacturer,
        model: model.model,
        minerCount: model.minerCount,
      }),
    );
  const displayedPairs = new Set([...livePairs, ...addedGroups.map(pairKey)]);
  const modelGroups: ReleaseChannelModelGroup[] = [
    ...liveGroups,
    ...addedGroups,
    ...Object.entries(staged)
      .filter(([key]) => !displayedPairs.has(key))
      .map(([, { group }]) => ({ ...group, minerCount: 0, onTargetCount: 0, activeRolloutId: 0n })),
  ];
  const activeCount = new Set(modelGroups.map((group) => activeForGroup(group)?.id).filter((id) => id !== undefined))
    .size;
  // Derived from the polled channel on every render so the open modal tracks
  // live firmware versions and phases; closes if the group empties.
  const minersGroup =
    minersPair !== null
      ? modelGroups.find((group) => observedPairKey(group) === minersPair && !group.rollbackPending)
      : undefined;
  const minersRollout = minersGroup ? activeForGroup(minersGroup) : undefined;
  const minersRolloutPending = minersGroup && minersGroup.activeRolloutId > 0n && !minersRollout;

  const dirtyAssignmentsByPair = new Map<string, AssignmentDraft>();
  const invalidSelections = new Map<string, string>();
  for (const group of modelGroups) {
    const key = pairKey(group);
    const fileId = staged[key]?.fileId;
    if (group.rollbackPending && fileId !== undefined) {
      invalidSelections.set(key, "Wait for the firmware assignment to refresh before applying changes.");
    }
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
  const savedSettingsDraft: ReleaseChannelDraft = {
    name: savedSettings?.name ?? "",
    description: savedSettings?.description ?? "",
    scope: savedSettings?.scope ?? create(ReleaseChannelScopeSchema),
    behavior: savedSettings?.behavior ?? create(RolloutBehaviorSchema),
  };
  const settingsChanges = dirty ? getChannelSettingsChanges(savedSettingsDraft, settingsDraft, minerNames) : null;
  const pendingChangeCount =
    dirtyAssignments.length +
    (settingsChanges ? settingsChanges.changes.length + settingsChanges.scopeChanges.length : 0);
  const hasUnsavedChanges = dirty || dirtyAssignments.length > 0;
  useLayoutEffect(() => {
    onDirtyChange?.(hasUnsavedChanges);
  }, [hasUnsavedChanges, onDirtyChange]);
  const assignmentCount = dirtyAssignments.filter((assignment) => assignment.firmwareFileId !== "").length;
  const clearCount = dirtyAssignments.length - assignmentCount;
  const exceedsAssignmentLimit = dirtyAssignments.length > MAX_FIRMWARE_ASSIGNMENTS;
  const effectiveSettings = dirty ? settingsDraft : savedSettings;
  const delegatedApplyBlocked = effectiveSettings?.behavior?.method === RolloutMethod.DELEGATED && assignmentCount > 0;
  const canApply =
    hasUnsavedChanges &&
    (!dirty || settingsValid) &&
    !delegatedApplyBlocked &&
    !exceedsAssignmentLimit &&
    invalidSelections.size === 0 &&
    !isWriting;

  const applyTitle = dirty
    ? "Apply channel changes?"
    : clearCount === 0
      ? "Start firmware update?"
      : assignmentCount === 0
        ? "Clear firmware assignments?"
        : "Apply firmware changes?";
  const applyButtonText = dirty
    ? "Apply changes"
    : clearCount === 0
      ? "Start update"
      : assignmentCount === 0
        ? "Clear assignments"
        : "Apply changes";
  const applySummary = [
    dirty
      ? dirtyAssignments.length > 0
        ? "Channel settings will be saved before the firmware changes are applied."
        : "Review the channel settings below."
      : "",
    assignmentCount > 0
      ? `Assign firmware for ${assignmentCount} ${assignmentCount === 1 ? "model" : "models"} in ${effectiveSettings?.name}. Updates start where needed. Pacing: ${pacingSummary(effectiveSettings?.behavior).toLowerCase()}.`
      : "",
    clearCount > 0
      ? `Clear firmware assignments for ${clearCount} ${clearCount === 1 ? "model" : "models"} in ${channel?.name}. Clearing stops enforcement and cancels remaining updates for these models; updates already dispatched may finish.`
      : "",
  ]
    .filter(Boolean)
    .join(" ");

  // Human-readable version for a staged file id, for the dialog summary.
  const versionLabel = (fileId: string): string => {
    if (fileId === "") return "No firmware";
    const file = firmwareFiles.find((f) => f.id === fileId);
    return file?.firmware_version || file?.filename || "unknown version";
  };
  // Original means the saved assignment, not a catalog label or one miner's
  // reported version. A successful write remains the baseline during read failures.
  const originalVersionLabel = (assignment: AssignmentDraft): string => {
    const key = pairKey(assignment);
    const acknowledged = acknowledgedAssignments[key];
    if (acknowledged) return acknowledged.firmwareFileId ? acknowledged.label : "No firmware";
    const group = modelGroups.find((group) => pairKey(group) === key);
    return group?.firmwareChecksum ? group.firmwareVersion || "Unknown version" : "No firmware";
  };

  const handleApply = async () => {
    if (writeInFlightRef.current || !channel || !canApply) return;
    if (writeLock && !writeLock.tryAcquire()) return;
    const submitted = dirtyAssignments.map((assignment) => ({
      ...assignment,
      label: versionLabel(assignment.firmwareFileId),
    }));
    const submittedSettings = dirty ? settingsDraft : null;
    const submittedSelections = staged;
    let settingsSaved = false;
    writeInFlightRef.current = true;
    setIsApplying(true);
    setApplyError(null);
    try {
      if (submittedSettings) {
        await onSave(submittedSettings);
        if (!mountedRef.current) return;
        settingsSaved = true;
        setSavedDraft({ settings: submittedSettings, beforeSave: latestChannelRef.current });
        setSettingsBase(submittedSettings);
      }
      const startedRollouts = submitted.length > 0 ? await onApply(channel.id, dirtyAssignments) : [];
      if (!mountedRef.current) return;
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
      if (submitted.length > 0)
        setAppliedAssignments((current) => ({
          assignments: { ...current?.assignments, ...acknowledged },
          beforeApply: latestChannelRef.current ?? channel,
        }));
      setStaged((current) => {
        const remaining = { ...current };
        for (const [key, selection] of Object.entries(submittedSelections)) {
          if (remaining[key] === selection) delete remaining[key];
        }
        return remaining;
      });
      setShowApplyDialog(false);
      pushToast({
        message: submittedSettings ? "Channel changes applied" : "Firmware changes applied",
        status: STATUSES.success,
      });
    } catch (error) {
      if (!mountedRef.current) return;
      const detail = error instanceof Error && error.message ? error.message : "Couldn't apply changes.";
      const message = settingsSaved
        ? `Channel settings were saved, but firmware changes could not be applied. Your firmware selections are still pending. ${detail}`
        : detail;
      setApplyError(message);
      pushToast({ message, status: STATUSES.error });
    } finally {
      writeInFlightRef.current = false;
      setIsApplying(false);
      writeLock?.release();
    }
  };

  const reviewChanges = () => {
    if (writeInFlightRef.current || !canApply) return;
    setShowSettings(false);
    setApplyError(null);
    setShowApplyDialog(true);
  };
  const discardChanges = () => {
    if (writeInFlightRef.current || isWriting) return;
    setName(savedSettings?.name ?? "");
    setDescription(savedSettings?.description ?? "");
    setScope(savedSettings?.scope ?? create(ReleaseChannelScopeSchema));
    setBehavior(savedSettings?.behavior ?? defaultBehavior());
    setNumberDraft({});
    setPreview(null);
    setStaged({});
    setApplyError(null);
    setShowApplyDialog(false);
  };

  const lastFinished = lastFinishedByChannelAssignment(channelRollouts);

  const settingsFields = (
    <div className="grid gap-8">
      <Section title="General">
        <div className="grid gap-3">
          <Input
            id="channel-name"
            label="Name"
            initValue={name}
            onChange={(value) => setName(value)}
            error={nameError}
            autoFocus={!channel}
            disabled={isWriting}
          />
          <Textarea
            id="channel-description"
            label="Description"
            initValue={description}
            onChange={(value) => setDescription(value)}
            error={descriptionError}
            disabled={isWriting}
          />
        </div>
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
          numberDraft={{ values: numberDraft, onChange: setNumberDraft }}
          allowDelegated={savedSettings?.behavior?.method === RolloutMethod.DELEGATED}
          disabled={isWriting}
        />
      </Section>

      <Section
        title="Applies to"
        subtext={
          channel
            ? "Choose which miners belong to this channel. Changes stay pending until you apply them."
            : "Choose which miners belong to this channel. Firmware can be assigned after creating it."
        }
      >
        <ScopeEditor
          scope={scope}
          onChange={setScope}
          previewScope={previewForChannel}
          onPreview={handlePreview}
          editingExistingChannel={channel !== undefined}
          disabled={isWriting}
        />
      </Section>
    </div>
  );

  if (!channel) {
    return (
      <Modal
        open
        title="Create release channel"
        testId="create-release-channel-modal"
        onDismiss={() => {
          if (!writeInFlightRef.current && !isWriting) onCancelCreate?.();
        }}
        buttons={[
          {
            text: "Create channel",
            variant: variants.primary,
            onClick: handleSave,
            disabled: !canSave,
            loading: isSaving,
            testId: "save-channel",
            dismissModalOnClick: false,
          },
        ]}
      >
        <div data-testid="release-channel-new">{settingsFields}</div>
      </Modal>
    );
  }

  return (
    <div className="flex flex-col gap-8" data-testid={`release-channel-${channel.name}`}>
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-3">
            <h2 className="text-heading-200 text-text-primary">{channel.name}</h2>
            {activeCount > 0 ? <UpdateActivePill count={activeCount} testId="channel-update-pill" /> : null}
          </div>
          {channel ? (
            <div className="flex items-center gap-2 text-200 text-text-primary-50">
              <span>{channel.minerCount === 1 ? "1 miner" : `${channel.minerCount.toLocaleString()} miners`}</span>
              <span>·</span>
              <span>{liveGroups.length === 1 ? "1 model" : `${liveGroups.length} models`}</span>
            </div>
          ) : null}
        </div>
        <div className="flex gap-2 phone:flex-col phone:items-stretch">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Channel settings"
            disabled={isWriting}
            onClick={() => {
              if (!writeInFlightRef.current) setShowSettings(true);
            }}
            testId="channel-settings"
          />
          {hasUnsavedChanges ? (
            <>
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Discard"
                disabled={isWriting}
                onClick={discardChanges}
              />
              <Button
                variant={variants.primary}
                size={sizes.compact}
                text="Apply changes"
                ariaLabel={`Apply changes (${pendingChangeCount})`}
                suffixIcon={
                  <span
                    aria-hidden="true"
                    data-testid="pending-change-count"
                    className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-surface-base px-1 text-emphasis-200 text-text-primary tabular-nums"
                  >
                    {pendingChangeCount}
                  </span>
                }
                disabled={!canApply}
                onClick={reviewChanges}
                testId="apply-firmware-changes"
              />
            </>
          ) : onShowHistory ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="History"
              onClick={() => onShowHistory(channel)}
              testId="channel-history"
            />
          ) : null}
        </div>
      </div>

      {channel ? (
        <Section
          title="Assigned miners"
          subtext="Grouped by hardware model. Choose a firmware version for each model, or view its individual miners."
        >
          {exceedsAssignmentLimit ? (
            <p role="alert" className="text-200 text-intent-critical-fill">
              {assignmentLimitMessage}
            </p>
          ) : null}
          {delegatedApplyBlocked ? (
            <p role="alert" className="text-200 text-intent-critical-fill" data-testid="delegated-apply-unavailable">
              {delegatedApplyMessage}
            </p>
          ) : null}
          {modelGroups.length === 0 ? (
            <span className="text-300 text-text-primary-50">
              No miners in scope yet. Open Channel settings and change Applies to to include miners.
            </span>
          ) : (
            <table aria-label="Assigned miners by model" className="w-full text-left text-200">
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
                  return (
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
                          stagedFileId={staged[pairKey(group)]?.fileId}
                          acknowledgedAssignment={acknowledged}
                          selectionError={invalidSelections.get(pairKey(group))}
                          disabled={isWriting}
                          onStageFirmware={(g, fileId) => {
                            if (!writeInFlightRef.current && !isWriting) {
                              const key = pairKey(g);
                              const savedFileId = acknowledged?.firmwareFileId ?? g.firmwareFileId;
                              const hasAssignment = acknowledged
                                ? acknowledged.firmwareFileId !== ""
                                : g.firmwareChecksum !== "";
                              setStaged((prev) => {
                                const next = { ...prev };
                                if (fileId === savedFileId && (fileId !== "" || !hasAssignment)) delete next[key];
                                else next[key] = { fileId, group: g };
                                return next;
                              });
                            }
                          }}
                        />
                      </td>
                      <td className="py-3 pr-4">
                        {acknowledged ? (
                          <span className="text-text-primary-50">Refreshing update status</span>
                        ) : !livePairs.has(pairKey(group)) ? (
                          <span className="text-text-primary-50">—</span>
                        ) : activeRollout ? (
                          <RolloutProgressIndicator
                            group={group}
                            rollout={activeRollout}
                            testId={`model-group-rollout-progress-${group.model}`}
                          />
                        ) : (
                          <ModelStatusCell
                            historyState={historyState}
                            group={group}
                            activeRollout={activeRollout}
                            lastFinished={lastFinished.get(channelAssignmentKey(channel.id, group))}
                          />
                        )}
                      </td>
                      <td className="py-3 text-right">
                        {group.minerCount > 0 ? (
                          <Button
                            variant={variants.secondary}
                            size={sizes.compact}
                            text="View miners"
                            disabled={
                              !livePairs.has(pairKey(group)) ||
                              acknowledged !== undefined ||
                              rolloutPending ||
                              group.rollbackPending
                            }
                            onClick={() => setMinersPair(observedPairKey(group))}
                            testId={`view-miners-${group.model}`}
                          />
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </Section>
      ) : null}

      {channel && showSettings ? (
        <Modal
          open
          title="Channel settings"
          description="Edit settings, then review and apply them with any pending firmware changes."
          testId="channel-settings-modal"
          onDismiss={() => {
            if (!writeInFlightRef.current && !isWriting) setShowSettings(false);
          }}
          buttons={[
            {
              text: "Review changes",
              variant: variants.primary,
              onClick: reviewChanges,
              disabled: !canApply,
              loading: isApplying,
              testId: "save-channel",
              dismissModalOnClick: false,
            },
          ]}
        >
          {settingsFields}
          {onDelete ? (
            <div className="mt-8 border-t border-border-10 pt-8">
              <Section
                title="Delete this channel"
                subtext="Miners keep their current firmware. Deleting this channel stops firmware enforcement and removes its update history."
              >
                <div className="flex flex-col items-start gap-3">
                  <Button
                    variant={variants.danger}
                    size={sizes.compact}
                    text="Delete channel"
                    disabled={hasUnsavedChanges || isWriting}
                    onClick={() => {
                      if (!writeInFlightRef.current && !isWriting && !hasUnsavedChanges) onDelete(channel);
                    }}
                    testId="delete-channel"
                  />
                  {hasUnsavedChanges ? (
                    <p className="text-200 text-text-primary-70">
                      Apply or discard your changes before deleting this channel.
                    </p>
                  ) : null}
                </div>
              </Section>
            </div>
          ) : null}
        </Modal>
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
          <p role="status" className="mb-4 text-200 text-text-primary-70">
            {pendingChangeCount} {pendingChangeCount === 1 ? "change" : "changes"} pending
          </p>
          {applyError ? (
            <p role="alert" className="mb-3 text-200 text-intent-critical-fill">
              {applyError}
            </p>
          ) : null}
          {dirty && !settingsValid ? (
            <p role="alert" className="mb-3 text-200 text-intent-critical-fill">
              Correct the channel settings before applying changes.
            </p>
          ) : null}
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
          {dirty && savedSettings ? (
            <div className="mb-5">
              <ChannelSettingsChanges before={savedSettingsDraft} after={settingsDraft} minerNames={minerNames} />
              {behaviorChanged && activeCount > 0 ? (
                <p className="mt-3 text-200 text-text-primary-70">
                  Updates already in progress keep their original behavior, except for the channel-wide offline limit.
                </p>
              ) : null}
              {scopeChanged ? (
                <p className="mt-3 text-200 text-text-primary-70">
                  Scope changes also affect current firmware assignments.
                </p>
              ) : null}
            </div>
          ) : null}
          {dirtyAssignments.length > 0 ? (
            <section aria-label="Firmware changes">
              <h3 className="mb-2 text-emphasis-300">Firmware</h3>
              <ChangePreviewTable
                label="Firmware changes"
                subject="Model"
                rows={dirtyAssignments.map((assignment) => ({
                  key: pairKey(assignment),
                  label: pairLabel(assignment),
                  original: originalVersionLabel(assignment),
                  target: versionLabel(assignment.firmwareFileId),
                }))}
              />
            </section>
          ) : null}
        </Dialog>
      ) : null}
    </div>
  );
};

export default ReleaseChannelManageView;
