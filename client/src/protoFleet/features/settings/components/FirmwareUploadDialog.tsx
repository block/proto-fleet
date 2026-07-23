import { useCallback, useEffect, useMemo, useState } from "react";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";
import {
  FileDropZone,
  FileErrorStatus,
  FileProcessingStatus,
  FileSelectedStatus,
  firmwareVersionFromFilename,
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
  const [pendingFile, setPendingFile] = useState<File | null>(null);

  const configLoaded = serverConfig !== null;
  const modelsLoading = open && modelGroups === null && modelsError === null;
  const modelsLoaded = !modelsLoading && modelsError === null;
  const isProcessing = state === "hashing" || state === "checking" || state === "uploading";
  const showLoadingSpinner = state === "idle" && (!configLoaded || modelsLoading);
  const showMetadataFields = configLoaded && modelsLoaded;
  const metadataLocked = state !== "idle";
  const showProcessingStatus = isProcessing && file != null;
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
  const hasCompleteTarget =
    target.targetManufacturer !== "" && target.targetModel !== "" && target.firmwareVersion !== "";

  const resetDialogState = useCallback((): void => {
    reset();
    setTargetManufacturer("");
    setTargetModel("");
    setFirmwareVersion("");
    setModelGroups(null);
    setModelsError(null);
    setPendingFile(null);
  }, [reset]);

  const handleDismiss = useCallback(() => {
    resetDialogState();
    onDismiss();
  }, [onDismiss, resetDialogState]);

  const handleUpload = useCallback((): void => {
    if (!pendingFile || !hasCompleteTarget) return;
    processFile(pendingFile, target, () => {
      resetDialogState();
      onSuccess();
    });
    setPendingFile(null);
  }, [hasCompleteTarget, onSuccess, pendingFile, processFile, resetDialogState, target]);

  const buttons = useMemo(() => {
    if (state === "idle" && pendingFile) {
      return [
        {
          text: "Upload",
          variant: variants.primary,
          onClick: handleUpload,
          dismissModalOnClick: false,
          disabled: !hasCompleteTarget,
        },
      ];
    }
    return undefined;
  }, [handleUpload, hasCompleteTarget, pendingFile, state]);

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

        {showMetadataFields ? (
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
                disabled={metadataLocked}
                forceBelow
              />
              <Select
                id="firmware-target-model"
                label="Model"
                options={modelOptions}
                value={selectedTargetModel}
                onChange={setTargetModel}
                disabled={metadataLocked || !targetManufacturer}
                forceBelow
              />
            </div>
            <Input
              id="firmware-version"
              label="Firmware version"
              initValue={firmwareVersion}
              onChange={setFirmwareVersion}
              disabled={metadataLocked}
              required
            />
            {state === "idle" ? (
              pendingFile ? (
                <FileSelectedStatus
                  fileName={pendingFile.name}
                  fileSize={pendingFile.size}
                  onRemove={() => setPendingFile(null)}
                />
              ) : (
                <FileDropZone
                  extensions={serverConfig.allowedExtensions}
                  onFileSelect={(selectedFile) => {
                    setPendingFile(selectedFile);
                    setFirmwareVersion((currentVersion) => {
                      if (currentVersion.trim()) return currentVersion;
                      return firmwareVersionFromFilename(selectedFile.name) ?? currentVersion;
                    });
                  }}
                />
              )
            ) : null}
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

        {showError ? <FileErrorStatus message={errorMessage} onRetry={retry} /> : null}
      </div>
    </Modal>
  );
};

export default FirmwareUploadDialog;
