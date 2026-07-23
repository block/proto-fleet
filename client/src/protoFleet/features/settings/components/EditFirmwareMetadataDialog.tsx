import { useEffect, useMemo, useState } from "react";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import type { FirmwareMetadataInput } from "@/protoFleet/api/useFirmwareApi";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";
import Select from "@/shared/components/Select";

interface EditableFirmwareFile {
  id: string;
  filename: string;
  targetManufacturer: string;
  targetModel: string;
  firmwareVersion: string;
}

interface EditFirmwareMetadataDialogProps {
  open: boolean;
  file: EditableFirmwareFile | null;
  isSubmitting: boolean;
  onConfirm: (metadata: FirmwareMetadataInput) => void;
  onDismiss: () => void;
}

const EditFirmwareMetadataDialog = ({
  open,
  file,
  isSubmitting,
  onConfirm,
  onDismiss,
}: EditFirmwareMetadataDialogProps) => {
  const { getMinerModelGroups } = useMinerModelGroups();
  const [targetManufacturer, setTargetManufacturer] = useState(file?.targetManufacturer ?? "");
  const [targetModel, setTargetModel] = useState(file?.targetModel ?? "");
  const [firmwareVersion, setFirmwareVersion] = useState(file?.firmwareVersion ?? "");
  const [modelGroups, setModelGroups] = useState<MinerModelGroup[] | null>(null);
  const [modelsError, setModelsError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || modelGroups !== null || modelsError !== null) return;
    let cancelled = false;
    void getMinerModelGroups(null)
      .then((groups) => {
        if (!cancelled) setModelGroups(groups);
      })
      .catch(() => {
        if (!cancelled) {
          setModelGroups([]);
          setModelsError("Couldn't load fleet miner models.");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [getMinerModelGroups, modelGroups, modelsError, open]);

  const manufacturerOptions = useMemo(() => {
    const manufacturers = new Set((modelGroups ?? []).map((group) => group.manufacturer.trim()).filter(Boolean));
    if (targetManufacturer.trim()) manufacturers.add(targetManufacturer.trim());
    return [
      { value: "", label: "Select manufacturer" },
      ...[...manufacturers].sort().map((manufacturer) => ({ value: manufacturer, label: manufacturer })),
    ];
  }, [modelGroups, targetManufacturer]);

  const modelOptions = useMemo(() => {
    const models = new Set(
      (modelGroups ?? [])
        .filter((group) => group.manufacturer.trim() === targetManufacturer.trim())
        .map((group) => group.model.trim())
        .filter(Boolean),
    );
    if (targetModel.trim()) models.add(targetModel.trim());
    return [
      { value: "", label: "Select model" },
      ...[...models].sort().map((model) => ({ value: model, label: model })),
    ];
  }, [modelGroups, targetManufacturer, targetModel]);

  const metadata = {
    targetManufacturer: targetManufacturer.trim(),
    targetModel: targetModel.trim(),
    firmwareVersion: firmwareVersion.trim(),
  };
  const canSubmit =
    modelGroups !== null &&
    modelsError === null &&
    metadata.targetManufacturer !== "" &&
    metadata.targetModel !== "" &&
    metadata.firmwareVersion !== "" &&
    !isSubmitting;

  return (
    <Modal
      open={Boolean(open && file !== null)}
      title="Edit firmware metadata"
      description={file ? file.filename : undefined}
      onDismiss={onDismiss}
      buttons={[
        {
          text: isSubmitting ? "Saving…" : "Save changes",
          variant: variants.primary,
          disabled: !canSubmit,
          dismissModalOnClick: false,
          onClick: () => onConfirm(metadata),
        },
      ]}
      divider={false}
      testId="edit-firmware-metadata-dialog"
    >
      <div className="mt-2 text-300 text-text-primary-70">Set the miner target and version for this firmware file.</div>
      <div className="mt-6 flex flex-col gap-4">
        {modelGroups === null && modelsError === null ? (
          <div className="flex items-center justify-center p-8">
            <ProgressCircular indeterminate size={24} />
          </div>
        ) : (
          <>
            <div className="grid gap-4 tablet:grid-cols-2">
              <Select
                id="edit-firmware-target-manufacturer"
                label="Manufacturer"
                options={manufacturerOptions}
                value={targetManufacturer}
                onChange={(value) => {
                  setTargetManufacturer(value);
                  setTargetModel("");
                }}
                disabled={isSubmitting}
                forceBelow
              />
              <Select
                id="edit-firmware-target-model"
                label="Model"
                options={modelOptions}
                value={targetModel}
                onChange={setTargetModel}
                disabled={isSubmitting || !targetManufacturer}
                forceBelow
              />
            </div>
            <Input
              id="edit-firmware-version"
              label="Firmware version"
              initValue={firmwareVersion}
              onChange={setFirmwareVersion}
              disabled={isSubmitting}
              required
            />
          </>
        )}
        {modelsError ? <Callout intent="danger" prefixIcon={<Alert />} title={modelsError} /> : null}
      </div>
    </Modal>
  );
};

export default EditFirmwareMetadataDialog;
