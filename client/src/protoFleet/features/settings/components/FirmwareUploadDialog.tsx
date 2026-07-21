import { useCallback, useEffect, useMemo, useState } from "react";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import {
  FileDropZone,
  FileErrorStatus,
  FileProcessingStatus,
  FileReadyStatus,
  useFirmwareUpload,
} from "@/protoFleet/components/FirmwareUpload";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";
import Select from "@/shared/components/Select";

interface FirmwareUploadDialogProps {
  open?: boolean;
  onSuccess: () => void;
  onDismiss: () => void;
}

const FirmwareUploadDialog = ({ open, onSuccess, onDismiss }: FirmwareUploadDialogProps) => {
  const { state, file, uploadProgress, errorMessage, serverConfig, processFile, reset, retry } =
    useFirmwareUpload(!!open);
  const { getMinerModelGroups } = useMinerModelGroups();
  const [targetManufacturer, setTargetManufacturer] = useState("");
  const [targetModel, setTargetModel] = useState("");
  const [firmwareVersion, setFirmwareVersion] = useState("");
  const [modelGroups, setModelGroups] = useState<MinerModelGroup[] | null>(null);
  const [modelsError, setModelsError] = useState<string | null>(null);

  const configLoaded = serverConfig !== null;
  const modelsLoading = open && modelGroups === null && modelsError === null;
  const modelsLoaded = !modelsLoading && modelsError === null;
  const isProcessing = state === "hashing" || state === "checking" || state === "uploading";
  const showLoadingSpinner = state === "idle" && (!configLoaded || modelsLoading);
  const showDropZone = state === "idle" && configLoaded && modelsLoaded;
  const showProcessingStatus = isProcessing && file != null;
  const showReadyStatus = state === "ready" && file != null;
  const showError = state === "error" && errorMessage != null;

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
    const manufacturers = [
      ...new Set((modelGroups ?? []).map((group) => group.manufacturer.trim()).filter(Boolean)),
    ].sort();
    return [
      { value: "", label: "Select manufacturer" },
      ...manufacturers.map((manufacturer) => ({ value: manufacturer, label: manufacturer })),
    ];
  }, [modelGroups]);

  const modelOptions = useMemo(() => {
    const models = (modelGroups ?? [])
      .filter((group) => group.manufacturer === targetManufacturer)
      .map((group) => group.model.trim())
      .filter(Boolean)
      .sort();
    return [
      { value: "", label: "Select model" },
      ...models.map((modelName) => ({ value: modelName, label: modelName })),
    ];
  }, [modelGroups, targetManufacturer]);

  const selectedTargetModel = modelOptions.some((option) => option.value === targetModel) ? targetModel : "";
  const target = useMemo(
    () => ({
      targetManufacturer: targetManufacturer.trim(),
      targetModel: selectedTargetModel.trim(),
      firmwareVersion: firmwareVersion.trim(),
    }),
    [firmwareVersion, targetManufacturer, selectedTargetModel],
  );

  const handleDismiss = useCallback(() => {
    const uploaded = state === "ready";
    reset();
    setTargetManufacturer("");
    setTargetModel("");
    setFirmwareVersion("");
    setModelGroups(null);
    setModelsError(null);
    if (uploaded) {
      onSuccess();
    } else {
      onDismiss();
    }
  }, [state, onDismiss, onSuccess, reset]);

  const handleDone = useCallback(() => {
    reset();
    setTargetManufacturer("");
    setTargetModel("");
    setFirmwareVersion("");
    setModelGroups(null);
    setModelsError(null);
    onSuccess();
  }, [onSuccess, reset]);

  const buttons =
    state === "ready"
      ? [{ text: "Done", variant: variants.primary, onClick: handleDone, dismissModalOnClick: false }]
      : undefined;

  return (
    <Modal open={open} title="Upload firmware" onDismiss={handleDismiss} buttons={buttons} divider={false}>
      <div className="mt-2 text-300 text-text-primary-70">
        Add a firmware file to make it available for miner updates.
      </div>
      <div className="mt-6 flex flex-col gap-4">
        {showLoadingSpinner ? (
          <div className="flex items-center justify-center p-8">
            <ProgressCircular indeterminate size={24} />
          </div>
        ) : null}

        {showDropZone ? (
          <>
            <div className="grid gap-4 tablet:grid-cols-2">
              <Select
                id="firmware-target-manufacturer"
                label="Manufacturer"
                options={manufacturerOptions}
                value={targetManufacturer}
                onChange={(value) => {
                  setTargetManufacturer(value);
                  setTargetModel("");
                }}
                forceBelow
              />
              <Select
                id="firmware-target-model"
                label="Model"
                options={modelOptions}
                value={selectedTargetModel}
                onChange={setTargetModel}
                disabled={!targetManufacturer}
                forceBelow
              />
            </div>
            <Input
              id="firmware-version"
              label="Firmware version"
              initValue={firmwareVersion}
              onChange={setFirmwareVersion}
              required
            />
            <FileDropZone
              extensions={serverConfig.allowedExtensions}
              onFileSelect={(selectedFile) => processFile(selectedFile, target)}
            />
          </>
        ) : null}

        {modelsError ? <Callout intent="danger" prefixIcon={<Alert />} title={modelsError} /> : null}

        {showProcessingStatus ? (
          <FileProcessingStatus
            state={state as "hashing" | "checking" | "uploading"}
            fileName={file.name}
            fileSize={file.size}
            uploadProgress={uploadProgress}
          />
        ) : null}

        {showReadyStatus ? <FileReadyStatus fileName={file.name} fileSize={file.size} /> : null}

        {showError ? <FileErrorStatus message={errorMessage} onRetry={retry} /> : null}
      </div>
    </Modal>
  );
};

export default FirmwareUploadDialog;
