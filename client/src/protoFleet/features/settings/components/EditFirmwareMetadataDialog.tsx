import { useState } from "react";
import type { FirmwareMetadataInput } from "@/protoFleet/api/useFirmwareApi";
import { hasCompleteFirmwareTarget } from "@/protoFleet/api/useFirmwareApi";
import FirmwareTargetFields from "@/protoFleet/features/settings/components/FirmwareTargetFields";
import { useMinerTargetOptions } from "@/protoFleet/features/settings/components/useMinerTargetOptions";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";

interface EditableFirmwareFile {
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
  const [targetManufacturer, setTargetManufacturer] = useState(file?.targetManufacturer ?? "");
  const [targetModel, setTargetModel] = useState(file?.targetModel ?? "");
  const [firmwareVersion, setFirmwareVersion] = useState(file?.firmwareVersion ?? "");
  const { modelGroups, modelsError, manufacturerOptions, modelOptions } = useMinerTargetOptions({
    active: open,
    selectedManufacturer: targetManufacturer,
    seedManufacturer: targetManufacturer,
    seedModel: targetModel,
  });

  const metadata = { targetManufacturer, targetModel, firmwareVersion };
  const canSubmit =
    modelGroups !== null && modelsError === null && hasCompleteFirmwareTarget(metadata) && !isSubmitting;

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
          <FirmwareTargetFields
            idPrefix="edit-firmware"
            manufacturerOptions={manufacturerOptions}
            modelOptions={modelOptions}
            manufacturer={targetManufacturer}
            model={targetModel}
            version={firmwareVersion}
            disabled={isSubmitting}
            onManufacturerChange={setTargetManufacturer}
            onModelChange={setTargetModel}
            onVersionChange={setFirmwareVersion}
          />
        )}
        {modelsError ? <Callout intent="danger" prefixIcon={<Alert />} title={modelsError} /> : null}
      </div>
    </Modal>
  );
};

export default EditFirmwareMetadataDialog;
