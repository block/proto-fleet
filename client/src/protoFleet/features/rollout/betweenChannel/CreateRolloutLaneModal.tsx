import { useMemo, useState } from "react";

import { type FirmwareFileInfo, hasCompleteFirmwareTarget } from "@/protoFleet/api/useFirmwareApi";
import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import type { RolloutLanePreview } from "@/protoFleet/features/rollout/rolloutTypes";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Radio from "@/shared/components/Radio";
import Textarea from "@/shared/components/Textarea";

export interface CreateRolloutLaneValues {
  label: string;
  description: string;
  firmwareFileIds: string[];
  deviceIdentifiers: string[];
  confirmInitialEnforcement: boolean;
  confirmReassignment: boolean;
  reassignmentConfirmationToken?: string;
}

type PreviewRolloutLaneValues = Pick<CreateRolloutLaneValues, "firmwareFileIds" | "deviceIdentifiers">;

interface CreateRolloutLaneModalProps {
  open: boolean;
  files: FirmwareFileInfo[];
  isSubmitting: boolean;
  error?: string | null;
  onDismiss: () => void;
  onPreview: (values: PreviewRolloutLaneValues) => Promise<RolloutLanePreview>;
  onCreate: (values: CreateRolloutLaneValues) => void;
}

export default function CreateRolloutLaneModal({
  open,
  files,
  isSubmitting,
  error,
  onDismiss,
  onPreview,
  onCreate,
}: CreateRolloutLaneModalProps) {
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [selectedFileByModel, setSelectedFileByModel] = useState<Record<string, string>>({});
  const [deviceIdentifiers, setDeviceIdentifiers] = useState<string[]>([]);
  const [showMinerSelection, setShowMinerSelection] = useState(false);
  const [confirmation, setConfirmation] = useState<{
    values: Omit<
      CreateRolloutLaneValues,
      "confirmInitialEnforcement" | "confirmReassignment" | "reassignmentConfirmationToken"
    >;
    preview: RolloutLanePreview;
  } | null>(null);
  const targetFiles = useMemo(
    () =>
      files.filter((file) =>
        hasCompleteFirmwareTarget({
          targetManufacturer: file.target_manufacturer,
          targetModel: file.target_model,
          firmwareVersion: file.firmware_version,
        }),
      ),
    [files],
  );
  const modelGroups = useMemo(() => {
    const groups = new Map<string, FirmwareFileInfo[]>();
    targetFiles.forEach((file) => {
      const key = minerTargetKey(file.target_manufacturer, file.target_model);
      if (key === null) {
        return;
      }
      groups.set(key, [...(groups.get(key) ?? []), file]);
    });
    return [...groups.entries()];
  }, [targetFiles]);
  const selectedFiles = Object.values(selectedFileByModel);
  const confirmationSourceLabels = confirmation
    ? [...new Set(confirmation.preview.reassignments.map((reassignment) => reassignment.sourceLaneLabel))]
    : [];
  const canCreate = label.trim().length > 0 && selectedFiles.length === 1;
  const selectedValues = (): Omit<
    CreateRolloutLaneValues,
    "confirmInitialEnforcement" | "confirmReassignment" | "reassignmentConfirmationToken"
  > => ({
    label: label.trim(),
    description: description.trim(),
    firmwareFileIds: selectedFiles,
    deviceIdentifiers,
  });
  const previewAndCreate = async () => {
    const values = selectedValues();
    if (values.deviceIdentifiers.length === 0) {
      onCreate({ ...values, confirmInitialEnforcement: false, confirmReassignment: false });
      return;
    }
    try {
      const preview = await onPreview({
        firmwareFileIds: values.firmwareFileIds,
        deviceIdentifiers: values.deviceIdentifiers,
      });
      if (preview.mismatchedCount > 0 || preview.unknownCount > 0 || preview.requiresReassignmentConfirmation) {
        setConfirmation({ values, preview });
        return;
      }
      onCreate({ ...values, confirmInitialEnforcement: false, confirmReassignment: false });
    } catch {
      // The API error remains visible in the lane creator.
    }
  };
  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [
    {
      text: isSubmitting ? "Creating..." : "Create lane",
      variant: variants.primary,
      disabled: !canCreate || isSubmitting,
      onClick: () => void previewAndCreate(),
    },
  ];
  const summary = (
    <div className="flex min-h-[280px] flex-col gap-6 rounded-3xl bg-surface-overlay p-8">
      <div>
        <div className="text-200 text-text-primary-50">Stable lane</div>
        <div className="mt-1 text-heading-200 text-text-primary">{label.trim() || "Untitled lane"}</div>
      </div>
      <div className="grid gap-4 tablet:grid-cols-2 laptop:grid-cols-1">
        <div>
          <div className="text-200 text-text-primary-50">Initial release</div>
          <div className="mt-1 text-emphasis-300 text-text-primary">
            {selectedFiles.length.toLocaleString()} model{selectedFiles.length === 1 ? "" : "s"}
          </div>
        </div>
        <div>
          <div className="text-200 text-text-primary-50">Initial membership</div>
          <div className="mt-1 text-emphasis-300 text-text-primary">
            {deviceIdentifiers.length.toLocaleString()} miner{deviceIdentifiers.length === 1 ? "" : "s"}
          </div>
        </div>
      </div>
      <div className="text-300 text-text-primary-70">
        Operators keep using this lane label while Fleet manages immutable version channels underneath it.
      </div>
    </div>
  );

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title="Create rollout lane"
        closeAriaLabel="Close rollout lane creator"
        onDismiss={onDismiss}
        isBusy={isSubmitting}
        buttons={buttons}
        abovePanes={<div className="px-6 pb-6 laptop:hidden">{summary}</div>}
        primaryPane={
          <div className="grid gap-10 pr-6 pb-8 laptop:pr-10">
            {error ? (
              <Callout
                intent={intents.danger}
                prefixIcon={<Alert />}
                title="Lane could not be created"
                subtitle={error}
              />
            ) : null}
            <section className="grid gap-3">
              <div className="text-emphasis-300 text-text-primary">General</div>
              <Input id="rollout-lane-label" label="Lane name" type="text" initValue={label} onChange={setLabel} />
              <Textarea
                id="rollout-lane-description"
                label="Description"
                rows={3}
                initValue={description}
                onChange={setDescription}
              />
            </section>

            <section className="grid gap-3">
              <div>
                <div className="text-emphasis-300 text-text-primary">Initial firmware</div>
                <div className="text-300 text-text-primary-70">
                  Choose one model and its initial firmware. Add more models after creating the lane.
                </div>
              </div>
              {modelGroups.length === 0 ? (
                <Callout
                  intent={intents.warning}
                  prefixIcon={<Alert />}
                  title="No compatible firmware files"
                  subtitle="Upload files with manufacturer, model, and version metadata before creating a lane."
                />
              ) : (
                <div className="grid gap-4">
                  {modelGroups.map(([key, groupFiles]) => (
                    <fieldset key={key} className="grid gap-2 rounded-xl border border-border-5 p-4">
                      <legend className="px-1 text-emphasis-300 text-text-primary">
                        {groupFiles[0].target_manufacturer} {groupFiles[0].target_model}
                      </legend>
                      {groupFiles.map((file) => (
                        <label key={file.id} className="flex cursor-pointer items-center gap-3 py-1 text-300">
                          <Radio
                            name={`lane-file-${encodeURIComponent(key)}`}
                            value={file.id}
                            selected={selectedFileByModel[key] === file.id}
                            onChange={() => setSelectedFileByModel({ [key]: file.id })}
                          />
                          <span className="min-w-0">
                            <span className="block truncate text-text-primary">{file.filename}</span>
                            <span className="block text-200 text-text-primary-70">{file.firmware_version}</span>
                          </span>
                        </label>
                      ))}
                    </fieldset>
                  ))}
                </div>
              )}
            </section>

            <section className="grid gap-3">
              <div className="flex items-center justify-between gap-4">
                <div>
                  <div className="text-emphasis-300 text-text-primary">Miners</div>
                  <div className="text-300 text-text-primary-70">
                    {deviceIdentifiers.length === 0
                      ? "0 miners selected"
                      : `${deviceIdentifiers.length.toLocaleString()} miners selected`}
                  </div>
                </div>
                <Button
                  text={deviceIdentifiers.length === 0 ? "Select miners" : "Change miners"}
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => setShowMinerSelection(true)}
                />
              </div>
            </section>
          </div>
        }
        secondaryPane={summary}
        secondaryPaneClassName="!hidden !bg-transparent laptop:!flex laptop:!pl-0"
      />
      <MinerSelectionModal
        open={showMinerSelection}
        selectedMinerIds={deviceIdentifiers}
        showRolloutLaneColumn
        onDismiss={() => setShowMinerSelection(false)}
        onSave={(selection) => {
          setDeviceIdentifiers(selection.selectedMinerIds);
          setShowMinerSelection(false);
        }}
      />
      <Dialog
        open={confirmation !== null}
        title="Review lane creation changes"
        testId="initial-enforcement-confirmation"
        onDismiss={() => setConfirmation(null)}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            onClick: () => setConfirmation(null),
            variant: variants.secondary,
            disabled: isSubmitting,
          },
          {
            text: "Confirm and create lane",
            onClick: () => {
              if (!confirmation) {
                return;
              }
              onCreate({
                ...confirmation.values,
                confirmInitialEnforcement:
                  confirmation.preview.mismatchedCount > 0 || confirmation.preview.unknownCount > 0,
                confirmReassignment: confirmation.preview.requiresReassignmentConfirmation,
                ...(confirmation.preview.requiresReassignmentConfirmation
                  ? { reassignmentConfirmationToken: confirmation.preview.reassignmentConfirmationToken }
                  : {}),
              });
            },
            variant: variants.primary,
            loading: isSubmitting,
          },
        ]}
      >
        {confirmation ? (
          <div className="grid gap-3 text-300 text-text-primary-70">
            {confirmation.preview.requiresReassignmentConfirmation ? (
              <p>
                {confirmation.preview.reassignments.length.toLocaleString()} selected{" "}
                {confirmation.preview.reassignments.length === 1 ? "miner is" : "miners are"} already assigned to{" "}
                {confirmationSourceLabels.join(", ")} and will be reassigned.
              </p>
            ) : null}
            {confirmation.preview.mismatchedCount > 0 || confirmation.preview.unknownCount > 0 ? (
              <>
                <p>
                  {confirmation.preview.mismatchedCount.toLocaleString()} mismatched{" "}
                  {confirmation.preview.mismatchedCount === 1 ? "miner" : "miners"} and{" "}
                  {confirmation.preview.unknownCount.toLocaleString()}{" "}
                  {confirmation.preview.unknownCount === 1 ? "miner" : "miners"} with unknown firmware require updates.
                </p>
                <p>
                  Creating the lane immediately starts firmware updates for{" "}
                  {(confirmation.preview.mismatchedCount + confirmation.preview.unknownCount).toLocaleString()} miners.
                  Matching miners are recorded as already confirmed.
                </p>
              </>
            ) : null}
          </div>
        ) : null}
      </Dialog>
    </>
  );
}
