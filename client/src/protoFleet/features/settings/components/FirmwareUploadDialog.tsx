import { useCallback, useEffect, useMemo, useState } from "react";
import { hasCompleteFirmwareTarget } from "@/protoFleet/api/useFirmwareApi";
import {
  FileDropZone,
  FileErrorStatus,
  FileProcessingStatus,
  FileSelectedStatus,
  firmwareVersionFromFilename,
  useFirmwareUpload,
} from "@/protoFleet/components/FirmwareUpload";
import FirmwareTargetFields from "@/protoFleet/features/settings/components/FirmwareTargetFields";
import { useMinerTargetOptions } from "@/protoFleet/features/settings/components/useMinerTargetOptions";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";

interface FirmwareUploadDialogProps {
  open?: boolean;
  onSuccess: () => void;
  onDismiss: () => void;
}

const FirmwareUploadDialog = ({ open, onSuccess, onDismiss }: FirmwareUploadDialogProps) => {
  const { state, file, uploadProgress, errorMessage, serverConfig, processFile, reset, retry } =
    useFirmwareUpload(!!open);
  const [targetManufacturer, setTargetManufacturer] = useState("");
  const [targetModel, setTargetModel] = useState("");
  const [firmwareVersion, setFirmwareVersion] = useState("");
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const {
    modelGroups,
    modelsError,
    manufacturerOptions,
    modelOptions,
    reset: resetModelOptions,
  } = useMinerTargetOptions({ active: !!open, selectedManufacturer: targetManufacturer });

  const configLoaded = serverConfig !== null;
  const modelsLoading = open && modelGroups === null && modelsError === null;
  const isProcessing = state === "hashing" || state === "checking" || state === "uploading";
  const showLoadingSpinner = state === "idle" && (!configLoaded || modelsLoading);
  const showMetadataFields = configLoaded && !modelsLoading && modelsError === null;
  const metadataLocked = state !== "idle";
  const showProcessingStatus = isProcessing && file != null;
  const showError = state === "error" && errorMessage != null;

  const selectedTargetModel = modelOptions.some((option) => option.value === targetModel) ? targetModel : "";
  const target = useMemo(
    () => ({ targetManufacturer, targetModel: selectedTargetModel, firmwareVersion }),
    [firmwareVersion, targetManufacturer, selectedTargetModel],
  );
  const hasCompleteTarget = hasCompleteFirmwareTarget(target);

  const resetDialogState = useCallback((): void => {
    reset();
    setTargetManufacturer("");
    setTargetModel("");
    setFirmwareVersion("");
    resetModelOptions();
    setPendingFile(null);
  }, [reset, resetModelOptions]);

  const handleDismiss = useCallback(() => {
    resetDialogState();
    onDismiss();
  }, [onDismiss, resetDialogState]);

  const handleUpload = useCallback((): void => {
    if (!pendingFile || !hasCompleteTarget) return;
    processFile(pendingFile, target);
    setPendingFile(null);
  }, [hasCompleteTarget, pendingFile, processFile, target]);

  useEffect(() => {
    if (state !== "ready") return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one-shot reset synchronizing dialog state with the upload hook's async completion
    resetDialogState();
    onSuccess();
  }, [onSuccess, resetDialogState, state]);

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
            <FirmwareTargetFields
              idPrefix="firmware"
              manufacturerOptions={manufacturerOptions}
              modelOptions={modelOptions}
              manufacturer={targetManufacturer}
              model={selectedTargetModel}
              version={firmwareVersion}
              disabled={metadataLocked}
              onManufacturerChange={setTargetManufacturer}
              onModelChange={setTargetModel}
              onVersionChange={setFirmwareVersion}
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
