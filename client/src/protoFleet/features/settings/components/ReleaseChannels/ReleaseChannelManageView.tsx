import { type ReactElement, type ReactNode, useCallback, useMemo, useState } from "react";
import { create, equals } from "@bufbuild/protobuf";

import { defaultBehavior } from "./behaviorUtils";
import { ModelStatusCell } from "./channelStatus";
import FirmwarePickerButton from "./FirmwarePickerButton";
import ModelMinersModal from "./ModelMinersModal";
import RolloutControls from "./RolloutControls";
import {
  isActive,
  isPaused,
  pacingSummary,
  rolloutDeviceCounts,
  rolloutProgressColorMap,
  rolloutProgressSegments,
  rolloutProgressSummary,
} from "./rolloutStatus";
import ScopeEditor from "./ScopeEditor";
import {
  type PreviewReleaseChannelScopeResponse,
  type ReleaseChannel,
  type ReleaseChannelModelGroup,
  type ReleaseChannelScope,
  ReleaseChannelScopeSchema,
  type Rollout,
  type RolloutBehavior,
  RolloutBehaviorSchema,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { ReleaseChannelDraft } from "@/protoFleet/api/useReleaseChannels";
import Button, { sizes, variants } from "@/shared/components/Button";
import CompositionBar from "@/shared/components/CompositionBar";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Textarea from "@/shared/components/Textarea";
import { pushToast, STATUSES } from "@/shared/features/toaster";

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

interface FirmwarePickerCellProps {
  group: ReleaseChannelModelGroup;
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
      testId={`channel-firmware-select-${group.model}`}
    />
  );
};

interface ReleaseChannelManageViewProps {
  // Undefined creates a new channel.
  channel?: ReleaseChannel;
  rollouts: Rollout[];
  firmwareFiles: FirmwareFileInfo[];
  minerNames: Record<string, string>;
  previewScope: (scope: ReleaseChannelScope, channelId?: bigint) => Promise<PreviewReleaseChannelScopeResponse>;
  onSave: (draft: ReleaseChannelDraft) => Promise<void>;
  onDelete?: (channel: ReleaseChannel) => void;
  onShowHistory?: (channel: ReleaseChannel) => void;
  onApply: (channelId: bigint, assignments: { model: string; firmwareFileId: string }[]) => Promise<void>;
}

// The per-channel management surface behind "Manage" (and the create flow):
// General, Applies to and Update behavior are saved together; Firmware is
// applied per model and starts an update paced by the saved behavior.
const ReleaseChannelManageView = ({
  channel,
  rollouts,
  firmwareFiles,
  minerNames,
  previewScope,
  onSave,
  onDelete,
  onShowHistory,
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

  // Staged (unapplied) firmware choices per model; absent key = server value.
  const [staged, setStaged] = useState<Record<string, string>>({});
  const [isApplying, setIsApplying] = useState(false);
  const [showApplyDialog, setShowApplyDialog] = useState(false);
  // Model whose miner table is open in the "View miners" modal.
  const [minersModel, setMinersModel] = useState<string | null>(null);

  const channelId = channel?.id;
  const previewForChannel = useCallback(
    (candidate: ReleaseChannelScope) => previewScope(candidate, channelId),
    [previewScope, channelId],
  );

  const dirty =
    channel === undefined ||
    name.trim() !== channel.name ||
    description.trim() !== channel.description ||
    !equals(ReleaseChannelScopeSchema, scope, channel.scope ?? create(ReleaseChannelScopeSchema)) ||
    !equals(RolloutBehaviorSchema, behavior, channel.behavior ?? create(RolloutBehaviorSchema));
  const hasConflicts = (preview?.conflicts.length ?? 0) > 0;
  const canSave = dirty && name.trim() !== "" && !hasConflicts && !isSaving;

  const handleSave = () => {
    setIsSaving(true);
    onSave({ name: name.trim(), description: description.trim(), scope, behavior })
      .then(() => {
        pushToast({
          message: channel ? "Release channel saved" : `Created release channel ${name.trim()}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't save the release channel", status: STATUSES.error });
      })
      .finally(() => setIsSaving(false));
  };

  const channelRollouts = channel ? rollouts.filter((r) => r.channelId === channel.id) : [];
  const activeByModel = new Map(channelRollouts.filter(isActive).map((r) => [r.model, r]));
  const activeCount = activeByModel.size;
  const modelGroups = channel?.modelGroups ?? [];
  // Derived from the polled channel on every render so the open modal tracks
  // live firmware versions and phases; closes if the group empties.
  const minersGroup = minersModel !== null ? modelGroups.find((group) => group.model === minersModel) : undefined;

  const stagedValue = (group: ReleaseChannelModelGroup): string =>
    staged[group.model] !== undefined ? staged[group.model] : group.firmwareFileId;

  const dirtyAssignments = modelGroups
    .filter((group) => stagedValue(group) !== group.firmwareFileId)
    .map((group) => ({ model: group.model, firmwareFileId: stagedValue(group) }));

  // Human-readable version for a staged file id, for the dialog summary.
  const versionLabel = (fileId: string): string => {
    if (fileId === "") return "no firmware";
    const file = firmwareFiles.find((f) => f.id === fileId);
    return file?.firmware_version || file?.filename || "unknown version";
  };

  const handleApply = () => {
    if (!channel) return;
    setIsApplying(true);
    onApply(channel.id, dirtyAssignments)
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

  const inScopeCount = preview?.minerCount ?? channel?.minerCount ?? 0;
  const lastFinishedByModel = new Map<string, Rollout>();
  for (const r of channelRollouts) {
    if (r.status !== RolloutStatus.COMPLETED && r.status !== RolloutStatus.COMPLETED_WITH_FAILURES) continue;
    if (!lastFinishedByModel.has(r.model)) lastFinishedByModel.set(r.model, r); // rollouts arrive newest first
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
          {channel && onShowHistory ? (
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="History"
              onClick={() => onShowHistory(channel)}
              testId="channel-history"
            />
          ) : null}
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
        <ScopeEditor scope={scope} onChange={setScope} previewScope={previewForChannel} onPreview={setPreview} />
      </Section>

      <Section title="Update behavior" subtext="How firmware updates are paced across the selected miners.">
        <RolloutControls behavior={behavior} onChange={setBehavior} inScopeCount={inScopeCount} />
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
                  const activeRollout = activeByModel.get(group.model);
                  const counts = activeRollout ? rolloutDeviceCounts(activeRollout) : undefined;
                  return [
                    <tr
                      key={group.model}
                      className="border-t border-border-5"
                      data-testid={`model-group-${group.model}`}
                    >
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
                        <ModelStatusCell
                          group={group}
                          activeRollout={activeRollout}
                          lastFinished={lastFinishedByModel.get(group.model)}
                        />
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
              <span className="text-300 text-text-primary">
                {dirtyAssignments.length === 1
                  ? "1 firmware change pending"
                  : `${dirtyAssignments.length} firmware changes pending`}
                {" — applying starts an update per model."}
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
                  testId="apply-firmware-changes"
                />
              </div>
            </div>
          ) : null}
        </Section>
      ) : null}

      {channel && minersGroup ? (
        <ModelMinersModal
          channelName={channel.name}
          group={minersGroup}
          activeRollout={activeByModel.get(minersGroup.model)}
          minerNames={minerNames}
          onClose={() => setMinersModel(null)}
        />
      ) : null}

      {channel ? (
        <Dialog
          open={showApplyDialog}
          title="Start firmware update?"
          subtitle={`One update starts per changed model in ${channel.name}. Pacing: ${pacingSummary(channel.behavior).toLowerCase()}.`}
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
              loading: isApplying,
            },
          ]}
        >
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
