import { useMemo, useState } from "react";

import { toError } from "@/protoFleet/api/requestErrors";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import type {
  CreateRolloutLaneModelDeclarationInput,
  PreviewRolloutLaneModelDeclarationInput,
  PublishRolloutLaneModelTargetInput,
} from "@/protoFleet/api/useRolloutApi";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import {
  isCompleteRolloutFirmwareFile,
  rolloutIdempotencyKey,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import type { PreviewModelDeclaration, RolloutLane } from "@/protoFleet/features/rollout/rolloutTypes";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";

interface ManageRolloutLaneDeclarationsModalProps {
  open: boolean;
  lane: RolloutLane;
  files: FirmwareFileInfo[];
  isSubmitting: boolean;
  error?: string | null;
  onDismiss: () => void;
  onPreview: (input: PreviewRolloutLaneModelDeclarationInput) => Promise<PreviewModelDeclaration>;
  onCreate: (input: CreateRolloutLaneModelDeclarationInput) => Promise<RolloutLane>;
  onPublish: (input: PublishRolloutLaneModelTargetInput) => Promise<RolloutLane>;
  onUpdated: (lane: RolloutLane, message: string) => void;
}

export default function ManageRolloutLaneDeclarationsModal({
  open,
  lane,
  files,
  isSubmitting,
  error,
  onDismiss,
  onPreview,
  onCreate,
  onPublish,
  onUpdated,
}: ManageRolloutLaneDeclarationsModalProps) {
  const [selectedFileId, setSelectedFileId] = useState("");
  const [deviceIdentifiers, setDeviceIdentifiers] = useState<string[]>([]);
  const [showMiners, setShowMiners] = useState(false);
  const [isPreviewing, setIsPreviewing] = useState(false);
  const [preview, setPreview] = useState<PreviewModelDeclaration | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const declaredByKey = useMemo(
    () => new Map(lane.models.map((model) => [minerTargetKey(model.manufacturer, model.model), model])),
    [lane.models],
  );
  const eligibleFiles = useMemo(
    () =>
      files.filter((file) => {
        if (!isCompleteRolloutFirmwareFile(file)) {
          return false;
        }
        const key = minerTargetKey(file.target_manufacturer, file.target_model);
        const declaration = key ? declaredByKey.get(key) : undefined;
        return !declaration || declaration.memberCount === 0;
      }),
    [declaredByKey, files],
  );
  const groupedFiles = useMemo(() => {
    const groups = new Map<string, FirmwareFileInfo[]>();
    eligibleFiles.forEach((file) => {
      const key = minerTargetKey(file.target_manufacturer, file.target_model);
      if (key) {
        groups.set(key, [...(groups.get(key) ?? []), file]);
      }
    });
    return [...groups.entries()];
  }, [eligibleFiles]);
  const selectedFile = eligibleFiles.find((file) => file.id === selectedFileId);
  const selectedKey = selectedFile ? minerTargetKey(selectedFile.target_manufacturer, selectedFile.target_model) : null;
  const existingDeclaration = selectedKey ? declaredByKey.get(selectedKey) : undefined;

  const submit = async () => {
    if (!selectedFile) {
      return;
    }
    if (existingDeclaration) {
      const updated = await onPublish({
        laneId: lane.id,
        laneModelId: existingDeclaration.id,
        expectedRevision: existingDeclaration.revision,
        firmwareFileId: selectedFile.id,
        idempotencyKey: rolloutIdempotencyKey("publish-model-target", lane.id, existingDeclaration.id),
        reason: "Publish zero-member rollout lane model target",
      });
      onUpdated(updated, `Published ${existingDeclaration.manufacturer} ${existingDeclaration.model}`);
      return;
    }
    setPreviewError(null);
    setIsPreviewing(true);
    try {
      const nextPreview = await onPreview({
        laneId: lane.id,
        firmwareFileId: selectedFile.id,
        deviceIdentifiers,
      });
      if (
        nextPreview.mismatchedCount > 0 ||
        nextPreview.unknownCount > 0 ||
        nextPreview.requiresReassignmentConfirmation
      ) {
        setPreview(nextPreview);
        return;
      }
      const updated = await createDeclaration(nextPreview);
      onUpdated(updated, `Added ${selectedFile.target_manufacturer} ${selectedFile.target_model}`);
    } catch (previewFailure) {
      setPreviewError(toError(previewFailure, "Couldn't preview the model declaration. Try again.").message);
    } finally {
      setIsPreviewing(false);
    }
  };

  const createDeclaration = (confirmedPreview: PreviewModelDeclaration) => {
    if (!selectedFile) {
      throw new Error("Select a firmware target.");
    }
    return onCreate({
      laneId: confirmedPreview.laneId,
      expectedRevision: 0n,
      firmwareFileId: selectedFile.id,
      deviceIdentifiers,
      idempotencyKey: rolloutIdempotencyKey("create-model-declaration", confirmedPreview.laneId, selectedFile.id),
      reason: "Add rollout lane model declaration",
      confirmInitialEnforcement: confirmedPreview.mismatchedCount > 0 || confirmedPreview.unknownCount > 0,
      confirmReassignment: confirmedPreview.requiresReassignmentConfirmation,
      reassignmentConfirmationToken: confirmedPreview.reassignmentConfirmationToken,
    });
  };

  return (
    <>
      <Modal open={open} title={`Manage ${lane.label} models`} size="large" onDismiss={onDismiss}>
        <div className="grid gap-6">
          {error ? (
            <Callout
              intent={intents.danger}
              prefixIcon={<Alert />}
              title="Model declaration could not be updated"
              subtitle={error}
            />
          ) : null}
          {previewError ? (
            <Callout
              intent={intents.danger}
              prefixIcon={<Alert />}
              title="Couldn’t preview the model declaration"
              subtitle={previewError}
            />
          ) : null}
          <div>
            <div className="text-emphasis-300 text-text-primary">Add model or publish an empty model target</div>
            <div className="mt-1 text-300 text-text-primary-70">
              Declared models with miners are excluded. Empty declarations can publish a new target without starting a
              rollout.
            </div>
          </div>
          {groupedFiles.length === 0 ? (
            <Callout
              intent={intents.warning}
              prefixIcon={<Alert />}
              title="No eligible firmware targets"
              subtitle="Upload firmware for an undeclared model, or remove a model's miners before publishing its target."
            />
          ) : (
            <div className="grid gap-4">
              {groupedFiles.map(([key, group]) => {
                const declaration = declaredByKey.get(key);
                return (
                  <fieldset key={key} className="rounded-xl border border-border-5 p-4">
                    <legend className="px-1 text-emphasis-300 text-text-primary">
                      {group[0].target_manufacturer} {group[0].target_model}
                      {declaration ? " · Empty declaration" : " · New model"}
                    </legend>
                    <div className="grid gap-2">
                      {group.map((file) => (
                        <label key={file.id} className="flex cursor-pointer items-center gap-3 py-1 text-300">
                          <Radio
                            name={`lane-model-file-${encodeURIComponent(key)}`}
                            value={file.id}
                            selected={selectedFileId === file.id}
                            onChange={() => {
                              setSelectedFileId(file.id);
                              setDeviceIdentifiers([]);
                            }}
                          />
                          <span>
                            <span className="block text-text-primary">{file.filename}</span>
                            <span className="block text-200 text-text-primary-70">{file.firmware_version}</span>
                          </span>
                        </label>
                      ))}
                    </div>
                  </fieldset>
                );
              })}
            </div>
          )}
          {selectedFile && !existingDeclaration ? (
            <div className="flex items-center justify-between gap-4">
              <div>
                <div className="text-emphasis-300 text-text-primary">Compatible miners (optional)</div>
                <div className="text-300 text-text-primary-70">
                  {deviceIdentifiers.length.toLocaleString()} selected
                </div>
              </div>
              <Button
                text={deviceIdentifiers.length ? "Change miners" : "Select miners"}
                variant={variants.secondary}
                size={sizes.compact}
                onClick={() => setShowMiners(true)}
              />
            </div>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button text="Cancel" variant={variants.secondary} disabled={isSubmitting} onClick={onDismiss} />
            <Button
              text={
                isPreviewing ? "Preparing preview..." : existingDeclaration ? "Publish target" : "Review and add model"
              }
              variant={variants.primary}
              disabled={!selectedFile || isSubmitting || isPreviewing}
              loading={isSubmitting}
              onClick={() => void submit().catch(() => undefined)}
            />
          </div>
        </div>
      </Modal>
      <MinerSelectionModal
        open={showMiners}
        selectedMinerIds={deviceIdentifiers}
        showRolloutLaneColumn
        onDismiss={() => setShowMiners(false)}
        onSave={(selection) => {
          setDeviceIdentifiers(selection.selectedMinerIds);
          setShowMiners(false);
        }}
      />
      <Dialog
        open={preview !== null}
        title="Review model declaration"
        onDismiss={() => setPreview(null)}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            disabled: isSubmitting,
            onClick: () => setPreview(null),
          },
          {
            text: "Confirm and add model",
            variant: variants.primary,
            loading: isSubmitting,
            onClick: () => {
              if (!preview || !selectedFile) {
                return;
              }
              void createDeclaration(preview)
                .then((updated) => {
                  setPreview(null);
                  onUpdated(updated, `Added ${selectedFile.target_manufacturer} ${selectedFile.target_model}`);
                })
                .catch(() => undefined);
            },
          },
        ]}
      >
        {preview ? (
          <div className="grid gap-2 text-300 text-text-primary-70">
            <p>{deviceIdentifiers.length.toLocaleString()} miners will be bound to this declaration.</p>
            <p>
              {preview.mismatchedCount.toLocaleString()} mismatched and {preview.unknownCount.toLocaleString()} unknown
              miners will begin initial setup convergence.
            </p>
            {preview.requiresReassignmentConfirmation ? (
              <p>{preview.reassignments.length.toLocaleString()} miners will move from another rollout lane.</p>
            ) : null}
          </div>
        ) : null}
      </Dialog>
    </>
  );
}
