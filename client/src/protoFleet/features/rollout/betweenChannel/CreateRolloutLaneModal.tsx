import { useMemo, useState } from "react";

import { type FirmwareFileInfo, hasCompleteFirmwareTarget } from "@/protoFleet/api/useFirmwareApi";
import FullScreenTwoPaneModal, {
  type FullScreenTwoPaneModalProps,
} from "@/protoFleet/components/FullScreenTwoPaneModal";
import { minerTargetKey } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/minerTarget";
import MinerSelectionModal from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Radio from "@/shared/components/Radio";
import Textarea from "@/shared/components/Textarea";

export interface CreateRolloutLaneValues {
  label: string;
  description: string;
  firmwareFileIds: string[];
  deviceIdentifiers: string[];
}

interface CreateRolloutLaneModalProps {
  open: boolean;
  files: FirmwareFileInfo[];
  isSubmitting: boolean;
  error?: string | null;
  onDismiss: () => void;
  onCreate: (values: CreateRolloutLaneValues) => void;
}

export default function CreateRolloutLaneModal({
  open,
  files,
  isSubmitting,
  error,
  onDismiss,
  onCreate,
}: CreateRolloutLaneModalProps) {
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [selectedFileByModel, setSelectedFileByModel] = useState<Record<string, string>>({});
  const [deviceIdentifiers, setDeviceIdentifiers] = useState<string[]>([]);
  const [showMinerSelection, setShowMinerSelection] = useState(false);
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
  const canCreate = label.trim().length > 0 && selectedFiles.length > 0 && deviceIdentifiers.length > 0;
  const buttons: NonNullable<FullScreenTwoPaneModalProps["buttons"]> = [
    {
      text: isSubmitting ? "Creating..." : "Create lane",
      variant: variants.primary,
      disabled: !canCreate || isSubmitting,
      onClick: () =>
        onCreate({
          label: label.trim(),
          description: description.trim(),
          firmwareFileIds: selectedFiles,
          deviceIdentifiers,
        }),
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
                  Choose one uploaded file for each model in the lane.
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
                            onChange={() => setSelectedFileByModel((current) => ({ ...current, [key]: file.id }))}
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
                      ? "Select the initial lane membership."
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
        onDismiss={() => setShowMinerSelection(false)}
        onSave={(selection) => {
          setDeviceIdentifiers(selection.selectedMinerIds);
          setShowMinerSelection(false);
        }}
      />
    </>
  );
}
