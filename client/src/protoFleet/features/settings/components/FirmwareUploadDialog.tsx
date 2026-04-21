import { useCallback } from "react";
import {
  FileDropZone,
  FileErrorStatus,
  FileProcessingStatus,
  FileReadyStatus,
  useFirmwareUpload,
} from "@/protoFleet/components/FirmwareUpload";
import { variants } from "@/shared/components/Button";
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

  const configLoaded = serverConfig !== null;
  const isProcessing = state === "hashing" || state === "checking" || state === "uploading";
  const showLoadingSpinner = state === "idle" && !configLoaded;
  const showDropZone = state === "idle" && configLoaded;
  const showProcessingStatus = isProcessing && file != null;
  const showReadyStatus = state === "ready" && file != null;
  const showError = state === "error" && errorMessage != null;

  const handleDismiss = useCallback(() => {
    const uploaded = state === "ready";
    reset();
    if (uploaded) {
      onSuccess();
    } else {
      onDismiss();
    }
  }, [state, onDismiss, onSuccess, reset]);

  const handleDone = useCallback(() => {
    reset();
    onSuccess();
  }, [onSuccess, reset]);

  const buttons =
    state === "ready"
      ? [{ text: "Done", variant: variants.primary, onClick: handleDone, dismissModalOnClick: false }]
      : undefined;

  return (
    <Modal open={open} title="Upload firmware" onDismiss={handleDismiss} buttons={buttons} divider={false}>
      <div className="text-text-secondary mt-2 text-300">
        Add a firmware file to make it available for miner updates.
      </div>
      <div className="mt-6 flex flex-col gap-4">
        {showLoadingSpinner && (
          <div className="flex items-center justify-center p-8">
            <ProgressCircular indeterminate size={24} />
          </div>
        )}

        {showDropZone && <FileDropZone extensions={serverConfig.allowedExtensions} onFileSelect={processFile} />}

        {showProcessingStatus && (
          <FileProcessingStatus
            state={state as "hashing" | "checking" | "uploading"}
            fileName={file.name}
            fileSize={file.size}
            uploadProgress={uploadProgress}
          />
        )}

        {showReadyStatus && <FileReadyStatus fileName={file.name} fileSize={file.size} />}

        {showError && <FileErrorStatus message={errorMessage} onRetry={retry} />}
      </div>
    </Modal>
  );
};

export default FirmwareUploadDialog;
