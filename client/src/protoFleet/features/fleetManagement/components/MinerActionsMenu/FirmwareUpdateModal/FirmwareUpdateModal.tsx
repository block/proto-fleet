import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import type { FirmwareFileInfo } from "@/protoFleet/api/useFirmwareApi";
import { useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import {
  FileDropZone,
  FileErrorStatus,
  FileProcessingStatus,
  FileReadyStatus,
  useFirmwareUpload,
} from "@/protoFleet/components/FirmwareUpload";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp, isoToEpochSeconds } from "@/shared/utils/formatTimestamp";

interface FirmwareUpdateModalProps {
  open?: boolean;
  target?: {
    targetManufacturer: string;
    targetModel: string;
  } | null;
  onConfirm: (firmwareFileId: string) => void;
  onDismiss: () => void;
}

const fileMatchesTarget = (
  file: FirmwareFileInfo,
  target: { targetManufacturer: string; targetModel: string } | null | undefined,
) => {
  const targetManufacturer = target?.targetManufacturer.trim() ?? "";
  const targetModel = target?.targetModel.trim() ?? "";
  if (!targetManufacturer || !targetModel) return false;
  return (
    file.target_manufacturer.trim().toLowerCase() === targetManufacturer.toLowerCase() &&
    file.target_model.trim().toLowerCase() === targetModel.toLowerCase()
  );
};

const FirmwareUpdateModal = ({ open, target, onConfirm, onDismiss }: FirmwareUpdateModalProps) => {
  const {
    state: uploadState,
    file: uploadFile,
    firmwareFileId: uploadedFileId,
    uploadProgress,
    errorMessage,
    serverConfig,
    processFile,
    reset,
    retry,
  } = useFirmwareUpload(!!open);
  const { listFirmwareFiles } = useFirmwareApi();

  const [existingFiles, setExistingFiles] = useState<FirmwareFileInfo[] | null>(null);
  const [selectedExistingFileId, setSelectedExistingFileId] = useState<string | null>(null);
  const [showUploadZone, setShowUploadZone] = useState(false);
  const [firmwareVersion, setFirmwareVersion] = useState("");

  const effectiveTargetManufacturer = target?.targetManufacturer ?? "";
  const effectiveTargetModel = target?.targetModel ?? "";
  const uploadTarget = useMemo(
    () => ({
      targetManufacturer: effectiveTargetManufacturer.trim(),
      targetModel: effectiveTargetModel.trim(),
      firmwareVersion: firmwareVersion.trim(),
    }),
    [effectiveTargetManufacturer, effectiveTargetModel, firmwareVersion],
  );
  const hasUploadTarget =
    uploadTarget.targetManufacturer !== "" && uploadTarget.targetModel !== "" && uploadTarget.firmwareVersion !== "";

  useEffect(() => {
    if (open) {
      let cancelled = false;
      listFirmwareFiles()
        .then((files) => {
          if (!cancelled) setExistingFiles(files);
        })
        .catch((error) => {
          if (cancelled) return;
          setExistingFiles([]);
          pushToast({
            message: error?.message || "Failed to load firmware files",
            status: STATUSES.error,
          });
        });
      return () => {
        cancelled = true;
      };
    }
  }, [open, listFirmwareFiles]);

  const handleSelectExistingFile = useCallback(
    (fileId: string) => {
      if (uploadState !== "idle" && uploadState !== "ready" && uploadState !== "error") return;
      reset();
      setSelectedExistingFileId(fileId);
    },
    [uploadState, reset],
  );

  const handleUploadFileSelect = useCallback(
    (file: File) => {
      setSelectedExistingFileId(null);
      setShowUploadZone(true);
      processFile(file, uploadTarget);
    },
    [processFile, uploadTarget],
  );

  const effectiveFirmwareFileId = selectedExistingFileId ?? uploadedFileId;
  const isReady = selectedExistingFileId != null || uploadState === "ready";
  const compatibleExistingFiles = useMemo(
    () => existingFiles?.filter((file) => fileMatchesTarget(file, target)) ?? null,
    [existingFiles, target],
  );
  const visibleExistingFiles = compatibleExistingFiles ?? [];

  const handleConfirm = useCallback(() => {
    if (effectiveFirmwareFileId) {
      onConfirm(effectiveFirmwareFileId);
      reset();
      setSelectedExistingFileId(null);
      setExistingFiles(null);
      setShowUploadZone(false);
      setFirmwareVersion("");
    }
  }, [effectiveFirmwareFileId, onConfirm, reset]);

  const handleDismiss = useCallback(() => {
    reset();
    setSelectedExistingFileId(null);
    setExistingFiles(null);
    setShowUploadZone(false);
    setFirmwareVersion("");
    onDismiss();
  }, [onDismiss, reset]);

  const isProcessing = uploadState === "hashing" || uploadState === "checking" || uploadState === "uploading";
  const missingTarget =
    !!open && (effectiveTargetManufacturer.trim() === "" || effectiveTargetModel.trim() === "");
  const configLoading = uploadState !== "error" && !serverConfig;
  const hasExistingFiles = visibleExistingFiles.length > 0;
  const showLoadingSpinner = !missingTarget && configLoading && !hasExistingFiles;

  const buttons = isReady ? [{ text: "Continue", variant: variants.primary, onClick: handleConfirm }] : undefined;

  return (
    <Modal open={open} title="Add firmware payload" onDismiss={handleDismiss} buttons={buttons} divider={false}>
      <div className="mt-2 text-300 text-text-primary-70">
        Select a firmware payload to update your miners. They will reboot automatically after installation completes.
      </div>
      <div className="mt-6 flex flex-col gap-4">
        {showLoadingSpinner ? (
          <div className="flex items-center justify-center p-8">
            <ProgressCircular indeterminate size={24} />
          </div>
        ) : null}

        {missingTarget ? (
          <div className="text-300 text-intent-warning-fill">
            Unable to determine the selected miners&apos; manufacturer and model. Close this dialog and try again.
          </div>
        ) : null}

        {hasExistingFiles ? (
          <div className="flex flex-col gap-2">
            <div className="text-300 text-text-primary">Select an existing firmware file</div>
            <div
              className="flex max-h-48 flex-col gap-1 overflow-y-auto"
              role="radiogroup"
              aria-label="Existing firmware files"
            >
              {visibleExistingFiles.map((f) => (
                <button
                  key={f.id}
                  type="button"
                  role="radio"
                  aria-checked={selectedExistingFileId === f.id}
                  className={clsx(
                    "flex cursor-pointer items-center gap-3 rounded-lg border p-3 text-left transition-colors",
                    selectedExistingFileId === f.id
                      ? "border-border-20 bg-surface-elevated-base"
                      : "border-border-5 hover:border-border-20",
                    isProcessing && "pointer-events-none opacity-50",
                  )}
                  onClick={() => handleSelectExistingFile(f.id)}
                  disabled={isProcessing}
                >
                  <div className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2 border-border-20">
                    {selectedExistingFileId === f.id ? (
                      <div className="h-2 w-2 rounded-full bg-core-primary-fill" />
                    ) : null}
                  </div>
                  <div className="flex min-w-0 flex-col">
                    <div className="truncate text-300 text-text-primary">{f.filename}</div>
                    <div className="text-200 text-text-primary-70">
                      {f.target_manufacturer} {f.target_model}
                      {f.firmware_version ? ` · ${f.firmware_version}` : ""} · {formatFileSize(f.size)} ·{" "}
                      {formatTimestamp(isoToEpochSeconds(f.uploaded_at))}
                    </div>
                  </div>
                </button>
              ))}
            </div>

            {serverConfig ? (
              <div className="flex items-center gap-3 py-2">
                <div className="h-px flex-1 bg-border-5" />
                <Button
                  variant={variants.secondary}
                  size={buttonSizes.compact}
                  text={showUploadZone ? "Hide upload" : "Upload new file"}
                  onClick={() => setShowUploadZone((prev) => !prev)}
                />
                <div className="h-px flex-1 bg-border-5" />
              </div>
            ) : null}
          </div>
        ) : null}

        {uploadState === "error" && errorMessage ? <FileErrorStatus message={errorMessage} onRetry={retry} /> : null}

        {!missingTarget && uploadState === "idle" && serverConfig && (!hasExistingFiles || showUploadZone) ? (
          <>
            <div className="grid gap-4 tablet:grid-cols-2">
              <Input
                id="firmware-update-target-manufacturer"
                label="Product"
                initValue={effectiveTargetManufacturer}
                disabled
                required
              />
              <Input
                id="firmware-update-target-model"
                label="Model"
                initValue={effectiveTargetModel}
                disabled
                required
              />
            </div>
            <Input
              id="firmware-update-version"
              label="Firmware version"
              initValue={firmwareVersion}
              onChange={setFirmwareVersion}
              required
            />
            <FileDropZone
              extensions={serverConfig.allowedExtensions}
              onFileSelect={handleUploadFileSelect}
              disabled={!hasUploadTarget}
            />
          </>
        ) : null}

        {isProcessing && uploadFile ? (
          <FileProcessingStatus
            state={uploadState as "hashing" | "checking" | "uploading"}
            fileName={uploadFile.name}
            fileSize={uploadFile.size}
            uploadProgress={uploadProgress}
          />
        ) : null}

        {uploadState === "ready" && uploadFile && !selectedExistingFileId ? (
          <FileReadyStatus fileName={uploadFile.name} fileSize={uploadFile.size} />
        ) : null}
      </div>
    </Modal>
  );
};

export default FirmwareUpdateModal;
