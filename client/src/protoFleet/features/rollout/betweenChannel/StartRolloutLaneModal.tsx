import { useMemo, useState } from "react";

import { type FirmwareFileInfo, hasCompleteFirmwareTarget } from "@/protoFleet/api/useFirmwareApi";
import type { CreateRolloutBatchInput } from "@/protoFleet/api/useRolloutApi";
import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import {
  buildManualBatches,
  evaluateTargetCompatibility,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import type { RolloutLane, RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import { Alert, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

export interface StartRolloutLaneValues {
  laneId: string;
  name: string;
  firmwareFileIds: string[];
  batches: CreateRolloutBatchInput[];
  reason: string;
}

interface StartRolloutLaneModalProps {
  open: boolean;
  lane: RolloutLane;
  files: FirmwareFileInfo[];
  isSubmitting: boolean;
  error?: string | null;
  onDismiss: () => void;
  onStart: (values: StartRolloutLaneValues) => void;
}

function hasCompleteFileTarget(file: FirmwareFileInfo): boolean {
  return hasCompleteFirmwareTarget({
    targetManufacturer: file.target_manufacturer,
    targetModel: file.target_model,
    firmwareVersion: file.firmware_version,
  });
}

function defaultTargetFiles(lane: RolloutLane, files: FirmwareFileInfo[]): Record<string, string> {
  return Object.fromEntries(
    lane.currentReleaseTargets.flatMap((source) => {
      const key = minerTargetKey(source.targetManufacturer, source.targetModel)!;
      const matching = files.filter(
        (file) => hasCompleteFileTarget(file) && minerTargetKey(file.target_manufacturer, file.target_model) === key,
      );
      const compatible =
        matching.find(
          (file) => file.id !== source.firmwareFileId && file.firmware_version !== source.firmwareVersion,
        ) ?? matching[0];
      return compatible ? [[key, compatible.id]] : [];
    }),
  );
}

function defaultConfig(memberCount: number): RolloutPlanConfig {
  return {
    processType: "firmware",
    strategy: memberCount > 1 ? "pilotThenContinue" : "batched",
    order: "random",
    maxConcurrentOffline: 1,
    batchSize: 1,
    pilotSize: 1,
    reviewAfterEachBatch: true,
    autoContinueOnHealthyTelemetry: false,
    scheduleType: "startNow",
  };
}

export default function StartRolloutLaneModal({
  open,
  lane,
  files,
  isSubmitting,
  error,
  onDismiss,
  onStart,
}: StartRolloutLaneModalProps) {
  const [name, setName] = useState("");
  const [reason, setReason] = useState("");
  const [selectedFileByModel, setSelectedFileByModel] = useState<Record<string, string>>(() =>
    defaultTargetFiles(lane, files),
  );
  const [config, setConfig] = useState(() => defaultConfig(lane.memberCount));
  const compatibility = useMemo(
    () => evaluateTargetCompatibility(lane.currentReleaseTargets, files, selectedFileByModel),
    [files, lane.currentReleaseTargets, selectedFileByModel],
  );
  const batches = useMemo(
    () =>
      buildManualBatches(lane.memberIdentifiers, {
        strategy: config.strategy === "batched" ? "batched" : "pilotThenContinue",
        batchSize: config.batchSize,
        pilotSize: config.pilotSize,
      }),
    [config.batchSize, config.pilotSize, config.strategy, lane.memberIdentifiers],
  );
  const targetFileIds = compatibility.flatMap((row) => (row.targetFileId ? [row.targetFileId] : []));
  const hasFreshMembership =
    lane.memberCount > 0 && lane.memberIdentifiers.length === lane.memberCount && lane.memberIdentifiers.length > 0;
  const compatibilityReady = compatibility.length > 0 && compatibility.every((row) => row.status === "compatible");
  const hasValidBatchPlan =
    batches.length > 0 &&
    batches.every((batch) => batch.members.length > 0) &&
    batches.reduce((count, batch) => count + batch.members.length, 0) === lane.memberCount;
  const canStart =
    name.trim().length > 0 && reason.trim().length > 0 && hasFreshMembership && compatibilityReady && hasValidBatchPlan;
  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [
    {
      text: isSubmitting ? "Starting..." : "Start rollout",
      variant: variants.primary,
      disabled: !canStart || isSubmitting,
      onClick: () =>
        onStart({
          laneId: lane.id,
          name: name.trim(),
          firmwareFileIds: targetFileIds,
          batches,
          reason: reason.trim(),
        }),
    },
  ];
  const preview = (
    <div className="flex min-h-[360px] flex-col gap-6 rounded-3xl bg-surface-overlay p-8">
      <div>
        <div className="text-200 text-text-primary-50">Stable lane</div>
        <div className="mt-1 text-heading-200 text-text-primary">{lane.label}</div>
        <div className="mt-1 text-300 text-text-primary-70">
          {lane.memberCount.toLocaleString()} frozen miner{lane.memberCount === 1 ? "" : "s"}
        </div>
      </div>
      <div className="grid gap-3">
        <div className="text-emphasis-300 text-text-primary">Source to target compatibility</div>
        {compatibility.map((row) => (
          <div key={row.key} className="rounded-xl border border-border-5 bg-surface-base p-4">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-emphasis-300 text-text-primary">
                  {row.manufacturer} {row.model}
                </div>
                <div className="mt-1 text-300 text-text-primary-70">
                  {row.sourceVersion} to {row.targetVersion ?? "Select a target"}
                </div>
              </div>
              {row.status === "compatible" ? (
                <Success ariaLabel="Compatible" className="shrink-0 text-intent-success-fill" />
              ) : (
                <Alert ariaLabel={row.status === "missing" ? "Missing target" : "No-op target"} className="shrink-0" />
              )}
            </div>
            {row.status === "missing" ? (
              <div className="mt-2 text-200 text-intent-critical-50">A target file is required for this model.</div>
            ) : null}
            {row.status === "noOp" ? (
              <div className="mt-2 text-200 text-intent-critical-50">
                This target matches the current release. Choose a different version.
              </div>
            ) : null}
          </div>
        ))}
      </div>
      <div className="text-300 text-text-primary-70">
        Membership moves only after fresh target firmware and hashing confirmation.
      </div>
    </div>
  );

  return (
    <FullScreenTwoPaneModal
      open={open}
      title="Start firmware rollout"
      closeAriaLabel="Close firmware rollout creator"
      onDismiss={onDismiss}
      isBusy={isSubmitting}
      buttons={buttons}
      abovePanes={<div className="px-6 pb-6 laptop:hidden">{preview}</div>}
      primaryPane={
        <div className="grid gap-10 pr-6 pb-8 laptop:pr-10">
          {error ? (
            <Callout
              intent={intents.danger}
              prefixIcon={<Alert />}
              title="Rollout could not be started"
              subtitle={error}
            />
          ) : null}
          {!hasFreshMembership ? (
            <Callout
              intent={intents.danger}
              prefixIcon={<Alert />}
              title="Fresh membership is unavailable"
              subtitle="Close this dialog and reload the lane before starting. Fleet will not create batches from stale counts."
            />
          ) : null}
          <section className="grid gap-3">
            <div className="text-emphasis-300 text-text-primary">Rollout details</div>
            <Input id="rollout-name" label="Name" type="text" initValue={name} onChange={setName} />
            <Textarea id="rollout-reason" label="Reason" rows={3} initValue={reason} onChange={setReason} />
          </section>

          <section className="grid gap-4">
            <div>
              <div className="text-emphasis-300 text-text-primary">Target release</div>
              <div className="text-300 text-text-primary-70">
                Select one uploaded target for every model in the current release.
              </div>
            </div>
            {lane.currentReleaseTargets.map((source) => {
              const key = minerTargetKey(source.targetManufacturer, source.targetModel)!;
              const options = files
                .filter(
                  (file) =>
                    hasCompleteFileTarget(file) && minerTargetKey(file.target_manufacturer, file.target_model) === key,
                )
                .map((file) => ({
                  value: file.id,
                  label: `${file.firmware_version} (${file.filename})`,
                }));
              const row = compatibility.find((item) => item.key === key);
              return (
                <Select
                  key={key}
                  id={`target-file-${encodeURIComponent(key)}`}
                  label={`${source.targetManufacturer} ${source.targetModel}`}
                  options={options}
                  value={selectedFileByModel[key] ?? ""}
                  placeholder="Select target file"
                  emptyMessage="No uploaded target for this model"
                  error={row?.status === "noOp" ? "Choose a different release" : row?.status === "missing"}
                  onChange={(fileId) => setSelectedFileByModel((current) => ({ ...current, [key]: fileId }))}
                  forceBelow
                />
              );
            })}
          </section>

          <RolloutControls
            config={config}
            onChange={(next) =>
              setConfig({
                ...next,
                reviewAfterEachBatch: true,
                autoContinueOnHealthyTelemetry: false,
                scheduleType: "startNow",
              })
            }
            inScopeCount={lane.memberCount}
            allowedStrategies={["pilotThenContinue", "batched"]}
            showTiming={false}
            allowAutomaticReview={false}
            showMaxConcurrentOffline={false}
          />
          <div className="text-200 text-text-primary-70">
            Every batch stops at the manual review gate before the next batch is admitted.
          </div>
        </div>
      }
      secondaryPane={preview}
      secondaryPaneClassName="!hidden !bg-transparent laptop:!flex laptop:!pl-0"
    />
  );
}
