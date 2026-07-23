import { useCallback, useEffect, useMemo, useState } from "react";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type FirmwareMetadataInput, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
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
  const { state, file, firmwareFileId, uploadProgress, errorMessage, serverConfig, processFile, reset, retry } =
    useFirmwareUpload(!!open);
  const { updateFirmwareMetadata } = useFirmwareApi();
  const { getMinerModelGroups } = useMinerModelGroups();
  const [targetManufacturer, setTargetManufacturer] = useState("");
  const [targetModel, setTargetModel] = useState("");
  const [firmwareVersion, setFirmwareVersion] = useState("");
  const [modelGroups, setModelGroups] = useState<MinerModelGroup[] | null>(null);
  const [modelsError, setModelsError] = useState<string | null>(null);
  const [isSavingMetadata, setIsSavingMetadata] = useState(false);
  const [metadataSaveError, setMetadataSaveError] = useState<string | null>(null);
  const [savedMetadata, setSavedMetadata] = useState<FirmwareMetadataInput | null>(null);

  const configLoaded = serverConfig !== null;
  const modelsLoading = open && modelGroups === null && modelsError === null;
  const modelsLoaded = !modelsLoading && modelsError === null;
  const isProcessing = state === "hashing" || state === "checking" || state === "uploading";
  const showLoadingSpinner = state === "idle" && (!configLoaded || modelsLoading);
  const showMetadataFields = configLoaded && modelsLoaded;
  const metadataLocked = (state !== "idle" && state !== "ready") || isSavingMetadata;
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
  const hasCompleteTarget =
    target.targetManufacturer !== "" && target.targetModel !== "" && target.firmwareVersion !== "";
  const hasMetadataChanges =
    state === "ready" &&
    (savedMetadata === null ||
      savedMetadata.targetManufacturer !== target.targetManufacturer ||
      savedMetadata.targetModel !== target.targetModel ||
      savedMetadata.firmwareVersion !== target.firmwareVersion);

  const resetDialogState = useCallback((): void => {
    reset();
    setTargetManufacturer("");
    setTargetModel("");
    setFirmwareVersion("");
    setModelGroups(null);
    setModelsError(null);
    setIsSavingMetadata(false);
    setMetadataSaveError(null);
    setSavedMetadata(null);
  }, [reset]);

  const handleDismiss = useCallback(() => {
    const uploaded = state === "ready";
    resetDialogState();
    if (uploaded) {
      onSuccess();
    } else {
      onDismiss();
    }
  }, [state, onDismiss, onSuccess, resetDialogState]);

  const handleDone = useCallback(async (): Promise<void> => {
    if (hasMetadataChanges) {
      if (!firmwareFileId || !hasCompleteTarget) return;
      setIsSavingMetadata(true);
      setMetadataSaveError(null);
      try {
        await updateFirmwareMetadata(firmwareFileId, target);
      } catch (error) {
        setMetadataSaveError(error instanceof Error ? error.message : "We couldn't update the metadata. Try again.");
        setIsSavingMetadata(false);
        return;
      }
    }

    resetDialogState();
    onSuccess();
  }, [
    firmwareFileId,
    hasCompleteTarget,
    hasMetadataChanges,
    onSuccess,
    resetDialogState,
    target,
    updateFirmwareMetadata,
  ]);

  const buttons = useMemo(() => {
    if (state !== "ready") return undefined;
    return [
      {
        text: isSavingMetadata ? "Saving…" : "Done",
        variant: variants.primary,
        onClick: (): void => {
          void handleDone();
        },
        dismissModalOnClick: false,
        disabled: isSavingMetadata || (hasMetadataChanges && (!hasCompleteTarget || !firmwareFileId)),
      },
    ];
  }, [firmwareFileId, handleDone, hasCompleteTarget, hasMetadataChanges, isSavingMetadata, state]);

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
                  setMetadataSaveError(null);
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
                onChange={(value) => {
                  setMetadataSaveError(null);
                  setTargetModel(value);
                }}
                disabled={metadataLocked || !targetManufacturer}
                forceBelow
              />
            </div>
            <Input
              id="firmware-version"
              label="Firmware version"
              initValue={firmwareVersion}
              onChange={(value) => {
                setMetadataSaveError(null);
                setFirmwareVersion(value);
              }}
              disabled={metadataLocked}
              required
            />
            {state === "idle" ? (
              <FileDropZone
                extensions={serverConfig.allowedExtensions}
                onFileSelect={(selectedFile) => {
                  setSavedMetadata(target);
                  processFile(selectedFile, target);
                }}
                disabled={!hasCompleteTarget}
              />
            ) : null}
          </>
        ) : null}

        {modelsError ? <Callout intent="danger" prefixIcon={<Alert />} title={modelsError} /> : null}

        {metadataSaveError ? <Callout intent="danger" prefixIcon={<Alert />} title={metadataSaveError} /> : null}

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
