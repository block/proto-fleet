import { useMemo, useState } from "react";

import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type { CreateRolloutBatchInput, StartRolloutLaneModelPlanInput } from "@/protoFleet/api/useRolloutApi";
import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import {
  buildManualBatches,
  evaluateTargetCompatibility,
  isCompleteRolloutFirmwareFile,
  isFirmwareConvergenceReady,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import RolloutControls from "@/protoFleet/features/rollout/RolloutControls";
import type { RolloutHashratePolicy, RolloutLane, RolloutPlanConfig } from "@/protoFleet/features/rollout/rolloutTypes";
import { Alert, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface StartRolloutLaneValuesBase {
  laneId: string;
  name: string;
  reason: string;
}

export type StartRolloutLaneValues = StartRolloutLaneValuesBase &
  (
    | {
        firmwareFileIds: string[];
        batches: CreateRolloutBatchInput[];
        hashratePolicy?: RolloutHashratePolicy;
        modelPlans?: never;
      }
    | {
        firmwareFileIds?: never;
        batches?: never;
        hashratePolicy?: never;
        modelPlans: Omit<StartRolloutLaneModelPlanInput, "modelStartKey">[];
      }
  );

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
  return isCompleteRolloutFirmwareFile(file);
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

function maxDropBasisPoints(value: string): number | null {
  const match = /^(\d+)(?:\.(\d+))?$/.exec(value);
  if (!match) {
    return null;
  }
  const wholePercent = Number(match[1]);
  const fractionalPercent = match[2] ?? "0";
  if (!/^\d0*$/.test(fractionalPercent)) {
    return null;
  }
  const tenthPercent = Number(fractionalPercent[0]);
  if (wholePercent > 100 || (wholePercent === 100 && tenthPercent > 0)) {
    return null;
  }
  return wholePercent * 100 + tenthPercent * 10;
}

function healthyDurationSeconds(value: string): number | null {
  if (!/^\d+$/.test(value)) {
    return null;
  }
  const seconds = Number(value);
  return seconds >= 10 && seconds <= 1_800 && seconds % 10 === 0 ? seconds : null;
}

function hashRatePolicyForModel(
  enabled: boolean,
  maxDropBasisPointsValue: number | null,
  healthyDurationSecondsValue: number | null,
): RolloutHashratePolicy | undefined {
  return enabled && maxDropBasisPointsValue !== null && healthyDurationSecondsValue !== null
    ? {
        maxDropBasisPoints: maxDropBasisPointsValue,
        healthyDurationSeconds: healthyDurationSecondsValue,
      }
    : undefined;
}

interface EvidencePolicyDraft {
  enabled: boolean;
  maxDropPercent: string;
  healthyDuration: string;
}

const defaultEvidencePolicyDraft = (): EvidencePolicyDraft => ({
  enabled: false,
  maxDropPercent: "0.1",
  healthyDuration: "30",
});

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
  const modelMode = lane.topologyEnabled && lane.models.length > 0;
  const initialModelId = lane.models.find((model) => model.memberCount > 0)?.id ?? "";
  const [selectedLaneModelIds, setSelectedLaneModelIds] = useState<string[]>(() =>
    initialModelId ? [initialModelId] : [],
  );
  const [selectedLaneModelId, setSelectedLaneModelId] = useState(initialModelId);
  const selectedModel = lane.models.find((model) => model.id === selectedLaneModelId);
  const selectedModels = lane.models.filter((model) => selectedLaneModelIds.includes(model.id));
  const selectedSources = useMemo(
    () =>
      modelMode
        ? selectedModels.flatMap((model) =>
            model.currentFirmwareTarget
              ? [
                  {
                    firmwareFileId: model.currentFirmwareTarget.firmwareFileId,
                    targetManufacturer: model.manufacturer,
                    targetModel: model.model,
                    firmwareVersion: model.currentFirmwareTarget.firmwareVersion,
                    sha256: model.currentFirmwareTarget.sha256,
                  },
                ]
              : [],
          )
        : lane.currentReleaseTargets,
    [lane.currentReleaseTargets, modelMode, selectedModels],
  );
  const selectedMemberIdentifiers = useMemo(
    () => (modelMode && selectedModel ? (selectedModel.memberIdentifiers ?? []) : lane.memberIdentifiers),
    [lane.memberIdentifiers, modelMode, selectedModel],
  );
  const selectedMemberCount = modelMode && selectedModel ? selectedModel.memberCount : lane.memberCount;
  const [legacyConfig, setLegacyConfig] = useState(() => defaultConfig(lane.memberCount));
  const [configByModel, setConfigByModel] = useState<Record<string, RolloutPlanConfig>>(() =>
    Object.fromEntries(lane.models.map((model) => [model.id, defaultConfig(model.memberCount)])),
  );
  const config = modelMode && selectedModel ? configByModel[selectedModel.id] : legacyConfig;
  const [policyDraftByModel, setPolicyDraftByModel] = useState<Record<string, EvidencePolicyDraft>>(() =>
    Object.fromEntries(lane.models.map((model) => [model.id, defaultEvidencePolicyDraft()])),
  );
  const [legacyPolicyDraft, setLegacyPolicyDraft] = useState(defaultEvidencePolicyDraft);
  const policyDraft =
    modelMode && selectedModel
      ? (policyDraftByModel[selectedModel.id] ?? defaultEvidencePolicyDraft())
      : legacyPolicyDraft;
  const compatibility = useMemo(
    () => evaluateTargetCompatibility(selectedSources, files, selectedFileByModel),
    [files, selectedFileByModel, selectedSources],
  );
  const batches = useMemo(
    () =>
      buildManualBatches(selectedMemberIdentifiers, {
        strategy: config.strategy === "batched" ? "batched" : "pilotThenContinue",
        batchSize: config.batchSize,
        pilotSize: config.pilotSize,
      }),
    [config.batchSize, config.pilotSize, config.strategy, selectedMemberIdentifiers],
  );
  const targetFileIds = compatibility.flatMap((row) => (row.targetFileId ? [row.targetFileId] : []));
  const hasFreshMembership = modelMode
    ? selectedModels.length > 0 &&
      selectedModels.every(
        (model) =>
          model.memberCount > 0 &&
          model.memberIdentifiers?.length === model.memberCount &&
          model.memberIdentifiers.length > 0,
      )
    : selectedMemberCount > 0 &&
      selectedMemberIdentifiers.length === selectedMemberCount &&
      selectedMemberIdentifiers.length > 0;
  const compatibilityReady = compatibility.length > 0 && compatibility.every((row) => row.status === "compatible");
  const convergenceReady = modelMode
    ? selectedModels.length > 0 &&
      selectedModels.every(
        (model) =>
          model.firmwareConvergence.totalCount > 0 &&
          model.firmwareConvergence.confirmedCount === model.firmwareConvergence.totalCount,
      )
    : isFirmwareConvergenceReady(lane);
  const modelBatches = useMemo(
    () =>
      Object.fromEntries(
        selectedModels.map((model) => {
          const modelConfig = configByModel[model.id] ?? defaultConfig(model.memberCount);
          return [
            model.id,
            buildManualBatches(model.memberIdentifiers ?? [], {
              strategy: modelConfig.strategy === "batched" ? "batched" : "pilotThenContinue",
              batchSize: modelConfig.batchSize,
              pilotSize: modelConfig.pilotSize,
            }),
          ];
        }),
      ),
    [configByModel, selectedModels],
  );
  const hasValidBatchPlan = modelMode
    ? selectedModels.every((model) => {
        const planned = modelBatches[model.id] ?? [];
        return (
          planned.length > 0 &&
          planned.every((batch) => batch.members.length > 0) &&
          planned.reduce((count, batch) => count + batch.members.length, 0) === model.memberCount
        );
      })
    : batches.length > 0 &&
      batches.every((batch) => batch.members.length > 0) &&
      batches.reduce((count, batch) => count + batch.members.length, 0) === selectedMemberCount;
  const showHashratePolicy = batches.length > 1;
  const parsedMaxDropBasisPoints = maxDropBasisPoints(policyDraft.maxDropPercent);
  const parsedHealthyDurationSeconds = healthyDurationSeconds(policyDraft.healthyDuration);
  const selectedTotalMemberCount = modelMode
    ? selectedModels.reduce((total, model) => total + model.memberCount, 0)
    : selectedMemberCount;
  const hasValidHashratePolicy = modelMode
    ? selectedModels.every((model) => {
        const draft = policyDraftByModel[model.id] ?? defaultEvidencePolicyDraft();
        return (
          (modelBatches[model.id]?.length ?? 0) <= 1 ||
          !draft.enabled ||
          (maxDropBasisPoints(draft.maxDropPercent) !== null && healthyDurationSeconds(draft.healthyDuration) !== null)
        );
      })
    : !showHashratePolicy ||
      !policyDraft.enabled ||
      (parsedMaxDropBasisPoints !== null && parsedHealthyDurationSeconds !== null);
  const canStart =
    name.trim().length > 0 &&
    reason.trim().length > 0 &&
    convergenceReady &&
    hasFreshMembership &&
    compatibilityReady &&
    hasValidBatchPlan &&
    hasValidHashratePolicy &&
    (!modelMode || selectedModels.length > 0);
  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [
    {
      text: isSubmitting ? "Starting..." : "Start rollout",
      variant: variants.primary,
      disabled: !canStart || isSubmitting,
      onClick: () => {
        const hashratePolicy =
          showHashratePolicy &&
          policyDraft.enabled &&
          parsedMaxDropBasisPoints !== null &&
          parsedHealthyDurationSeconds !== null
            ? {
                maxDropBasisPoints: parsedMaxDropBasisPoints,
                healthyDurationSeconds: parsedHealthyDurationSeconds,
              }
            : undefined;
        if (modelMode && selectedModels.length > 0) {
          onStart({
            laneId: lane.id,
            name: name.trim(),
            reason: reason.trim(),
            modelPlans: selectedModels.map((model) => {
              const key = minerTargetKey(model.manufacturer, model.model)!;
              const modelDraft = policyDraftByModel[model.id] ?? defaultEvidencePolicyDraft();
              const modelPolicy = hashRatePolicyForModel(
                modelDraft.enabled,
                maxDropBasisPoints(modelDraft.maxDropPercent),
                healthyDurationSeconds(modelDraft.healthyDuration),
              );
              return {
                laneModelId: model.id,
                expectedModelRevision: model.revision,
                firmwareFileId: selectedFileByModel[key],
                batches: modelBatches[model.id] ?? [],
                ...(modelPolicy ? { hashratePolicy: modelPolicy } : {}),
              };
            }),
          });
        } else {
          onStart({
            laneId: lane.id,
            name: name.trim(),
            firmwareFileIds: targetFileIds,
            batches,
            reason: reason.trim(),
            ...(hashratePolicy ? { hashratePolicy } : {}),
          });
        }
      },
    },
  ];
  const preview = (
    <div className="flex min-h-[360px] flex-col gap-6 rounded-3xl bg-surface-overlay p-8">
      <div>
        <div className="text-200 text-text-primary-50">Stable lane</div>
        <div className="mt-1 text-heading-200 text-text-primary">{lane.label}</div>
        <div className="mt-1 text-300 text-text-primary-70">
          {selectedTotalMemberCount.toLocaleString()} frozen miner{selectedTotalMemberCount === 1 ? "" : "s"}
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
          {lane.memberCount === 0 ? (
            <Callout intent={intents.danger} prefixIcon={<Alert />} title="Add miners before starting a rollout." />
          ) : !hasFreshMembership ? (
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
                {modelMode
                  ? "Select one or more non-empty models. Each rollout is controlled independently."
                  : "Select one uploaded target for every model in the current release."}
              </div>
            </div>
            {modelMode
              ? lane.models.map((model) => (
                  <label key={model.id} className="flex items-center gap-3 rounded-xl border border-border-5 p-4">
                    <Checkbox
                      checked={selectedLaneModelIds.includes(model.id)}
                      disabled={isSubmitting || model.memberCount === 0}
                      onChange={() => {
                        setSelectedLaneModelIds((current) =>
                          current.includes(model.id)
                            ? current.filter((candidate) => candidate !== model.id)
                            : [...current, model.id],
                        );
                        setSelectedLaneModelId(model.id);
                      }}
                    />
                    <span>
                      <span className="block text-emphasis-300 text-text-primary">
                        {model.manufacturer} {model.model}
                      </span>
                      <span className="block text-200 text-text-primary-70">
                        {model.currentFirmwareTarget?.firmwareVersion ?? "No current target"} ·{" "}
                        {model.memberCount.toLocaleString()} miner{model.memberCount === 1 ? "" : "s"}
                      </span>
                    </span>
                  </label>
                ))
              : null}
            {selectedSources.map((source) => {
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

          {modelMode && selectedModel ? (
            <div className="text-emphasis-300 text-text-primary">
              Batch and evidence settings for {selectedModel.manufacturer} {selectedModel.model}
            </div>
          ) : null}
          <RolloutControls
            config={config}
            onChange={(next) =>
              modelMode && selectedModel
                ? setConfigByModel((current) => ({
                    ...current,
                    [selectedModel.id]: {
                      ...next,
                      reviewAfterEachBatch: true,
                      autoContinueOnHealthyTelemetry: false,
                      scheduleType: "startNow",
                    },
                  }))
                : setLegacyConfig({
                    ...next,
                    reviewAfterEachBatch: true,
                    autoContinueOnHealthyTelemetry: false,
                    scheduleType: "startNow",
                  })
            }
            inScopeCount={selectedMemberCount}
            allowedStrategies={["pilotThenContinue", "batched"]}
            showTiming={false}
            allowAutomaticReview={false}
            showMaxConcurrentOffline={false}
          />
          {showHashratePolicy ? (
            <section className="grid gap-3">
              <label
                className={`flex items-center gap-3 text-left ${
                  isSubmitting ? "cursor-not-allowed" : "cursor-pointer"
                }`}
              >
                <Checkbox
                  checked={policyDraft.enabled}
                  disabled={isSubmitting}
                  onChange={(event) => {
                    const enabled = event.currentTarget.checked;
                    if (modelMode && selectedModel) {
                      setPolicyDraftByModel((current) => ({
                        ...current,
                        [selectedModel.id]: {
                          ...(current[selectedModel.id] ?? defaultEvidencePolicyDraft()),
                          enabled,
                        },
                      }));
                      return;
                    }
                    setLegacyPolicyDraft((current) => ({ ...current, enabled }));
                  }}
                />
                <span className="text-300 text-text-primary">Auto-continue healthy batches</span>
              </label>
              {policyDraft.enabled ? (
                <div className="grid gap-3" data-testid="hashrate-policy-fields">
                  <Input
                    key={`max-drop-${selectedModel?.id ?? "legacy"}`}
                    id="rollout-hashrate-max-drop"
                    label="Maximum hashrate drop"
                    type="number"
                    inputMode="decimal"
                    units="%"
                    initValue={policyDraft.maxDropPercent}
                    error={parsedMaxDropBasisPoints === null ? "Enter 0 to 100% in 0.1% increments." : false}
                    disabled={isSubmitting}
                    onChange={(maxDropPercentValue) => {
                      if (modelMode && selectedModel) {
                        setPolicyDraftByModel((current) => ({
                          ...current,
                          [selectedModel.id]: {
                            ...(current[selectedModel.id] ?? defaultEvidencePolicyDraft()),
                            maxDropPercent: maxDropPercentValue,
                          },
                        }));
                        return;
                      }
                      setLegacyPolicyDraft((current) => ({ ...current, maxDropPercent: maxDropPercentValue }));
                    }}
                  />
                  <Input
                    key={`healthy-duration-${selectedModel?.id ?? "legacy"}`}
                    id="rollout-hashrate-healthy-duration"
                    label="Healthy duration"
                    type="number"
                    inputMode="numeric"
                    units="sec"
                    initValue={policyDraft.healthyDuration}
                    error={
                      parsedHealthyDurationSeconds === null
                        ? "Enter 10 to 1,800 seconds in 10-second increments."
                        : false
                    }
                    disabled={isSubmitting}
                    onChange={(healthyDurationValue) => {
                      if (modelMode && selectedModel) {
                        setPolicyDraftByModel((current) => ({
                          ...current,
                          [selectedModel.id]: {
                            ...(current[selectedModel.id] ?? defaultEvidencePolicyDraft()),
                            healthyDuration: healthyDurationValue,
                          },
                        }));
                        return;
                      }
                      setLegacyPolicyDraft((current) => ({ ...current, healthyDuration: healthyDurationValue }));
                    }}
                  />
                </div>
              ) : null}
            </section>
          ) : null}
          <div className="text-200 text-text-primary-70">
            {policyDraft.enabled && showHashratePolicy
              ? "Healthy batches continue automatically after the configured evidence window."
              : "Every batch stops at the manual review gate before the next batch is admitted."}
          </div>
        </div>
      }
      secondaryPane={preview}
      secondaryPaneClassName="!hidden !bg-transparent laptop:!flex laptop:!pl-0"
    />
  );
}
