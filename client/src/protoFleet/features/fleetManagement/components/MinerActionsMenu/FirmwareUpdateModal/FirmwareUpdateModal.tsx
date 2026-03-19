import type { ChangeEvent, DragEvent, RefObject } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";
import type { FirmwareConfig } from "@/protoFleet/api/useFirmwareApi";
import { computeSha256, useFirmwareApi, validateFirmwareFile } from "@/protoFleet/api/useFirmwareApi";
import { ArrowUp, Checkmark } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import Modal from "@/shared/components/Modal/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";

type ModalState = "idle" | "hashing" | "checking" | "uploading" | "ready" | "error";

interface FirmwareUpdateModalProps {
  open?: boolean;
  onConfirm: (firmwareFileId: string) => void;
  onDismiss: () => void;
}

const MIME_TYPES_BY_EXT: Record<string, string[]> = {
  ".tar.gz": ["application/gzip", "application/x-gzip", ".gz"],
  ".zip": ["application/zip"],
};

function buildAcceptString(extensions: string[]): string {
  const parts = new Set<string>();
  for (const ext of extensions) {
    parts.add(ext);
    for (const mime of MIME_TYPES_BY_EXT[ext] ?? []) parts.add(mime);
  }
  return [...parts].join(",");
}

interface FileDropZoneProps {
  extensions: string[];
  isDragActive: boolean;
  onClick: () => void;
  onDragEnter: (e: DragEvent) => void;
  onDragOver: (e: DragEvent) => void;
  onDragLeave: (e: DragEvent) => void;
  onDrop: (e: DragEvent) => void;
  onFileSelect: (e: ChangeEvent<HTMLInputElement>) => void;
  fileInputRef: RefObject<HTMLInputElement | null>;
}

function FileDropZone({
  extensions,
  isDragActive,
  onClick,
  onDragEnter,
  onDragOver,
  onDragLeave,
  onDrop,
  onFileSelect,
  fileInputRef,
}: FileDropZoneProps) {
  return (
    <>
      <div
        role="button"
        tabIndex={0}
        className={clsx(
          "flex cursor-pointer flex-col items-center justify-center gap-3 rounded-lg border-2 border-dashed p-8 transition-colors",
          isDragActive ? "border-border-focus bg-surface-elevated-base" : "border-border-5 hover:border-border-20",
        )}
        onClick={onClick}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onClick();
          }
        }}
        onDragEnter={onDragEnter}
        onDragOver={onDragOver}
        onDragLeave={onDragLeave}
        onDrop={onDrop}
        data-testid="firmware-drop-zone"
      >
        <ArrowUp className="text-text-secondary" />
        <div className="text-center">
          <div className="text-300 text-text-primary">Drag and drop a firmware file here, or click to browse</div>
          <div className="text-text-secondary mt-1 text-200">Accepted formats: {extensions.join(", ")}</div>
        </div>
      </div>
      <input
        ref={fileInputRef}
        type="file"
        accept={buildAcceptString(extensions)}
        onChange={onFileSelect}
        className="hidden"
        data-testid="firmware-file-input"
      />
    </>
  );
}

interface FileProcessingStatusProps {
  state: "hashing" | "checking" | "uploading";
  fileName: string;
  fileSize: number;
  uploadProgress: number;
}

function FileProcessingStatus({ state, fileName, fileSize, uploadProgress }: FileProcessingStatusProps) {
  return (
    <div className="flex items-center gap-4 rounded-lg border border-border-5 p-4">
      {state === "uploading" ? (
        <ProgressCircular value={uploadProgress} size={24} />
      ) : (
        <ProgressCircular indeterminate size={24} />
      )}
      <div className="flex flex-col">
        <div className="text-300 text-text-primary">{fileName}</div>
        <div className="text-text-secondary text-200">
          {state === "hashing" && "Computing checksum..."}
          {state === "checking" && "Checking server..."}
          {state === "uploading" && `${uploadProgress}% uploaded · ${formatFileSize(fileSize)}`}
        </div>
      </div>
    </div>
  );
}

interface FileReadyStatusProps {
  fileName: string;
  fileSize: number;
}

function FileReadyStatus({ fileName, fileSize }: FileReadyStatusProps) {
  return (
    <div className="flex items-center gap-4 rounded-lg border border-border-5 p-4">
      <Checkmark className="text-intent-success-fill" />
      <div className="flex flex-col">
        <div className="text-300 text-text-primary">{fileName}</div>
        <div className="text-text-secondary text-200">{formatFileSize(fileSize)} · Ready</div>
      </div>
    </div>
  );
}

interface FileErrorStatusProps {
  message: string;
  onRetry: () => void;
}

function FileErrorStatus({ message, onRetry }: FileErrorStatusProps) {
  return (
    <div className="flex flex-col gap-3">
      <div className="text-300 text-intent-warning-fill">{message}</div>
      <button type="button" className="text-text-link cursor-pointer text-300 underline" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}

const FirmwareUpdateModal = ({ open, onConfirm, onDismiss }: FirmwareUpdateModalProps) => {
  const [state, setState] = useState<ModalState>("idle");
  const [file, setFile] = useState<File | null>(null);
  const [firmwareFileId, setFirmwareFileId] = useState<string | null>(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [isDragActive, setIsDragActive] = useState(false);
  const [serverConfig, setServerConfig] = useState<FirmwareConfig | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  const { getConfig, checkFirmwareFile, uploadFirmwareFile } = useFirmwareApi();

  useEffect(() => {
    if (open) {
      void getConfig()
        .then(setServerConfig)
        .catch((err) => {
          setErrorMessage(err instanceof Error ? err.message : "Failed to load firmware configuration.");
          setState("error");
        });
    }
  }, [open, getConfig]);

  const resetState = useCallback(() => {
    abortControllerRef.current?.abort();
    abortControllerRef.current = null;
    setState("idle");
    setFile(null);
    setFirmwareFileId(null);
    setUploadProgress(0);
    setErrorMessage(null);
    setIsDragActive(false);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }, []);

  const processFile = useCallback(
    async (selectedFile: File) => {
      abortControllerRef.current?.abort();
      const controller = new AbortController();
      abortControllerRef.current = controller;

      try {
        const config = serverConfig ?? (await getConfig());
        if (controller.signal.aborted) return;

        const validationError = validateFirmwareFile(selectedFile, config);
        if (validationError) {
          setErrorMessage(validationError);
          setState("error");
          return;
        }

        setFile(selectedFile);
        setState("hashing");
        const sha256 = await computeSha256(selectedFile);
        if (controller.signal.aborted) return;

        setState("checking");
        const { exists, firmwareFileId: existingId } = await checkFirmwareFile(sha256, controller.signal);

        if (exists && existingId) {
          setFirmwareFileId(existingId);
          setState("ready");
          return;
        }

        setState("uploading");
        setUploadProgress(0);
        const newId = await uploadFirmwareFile(selectedFile, {
          onProgress: setUploadProgress,
          signal: controller.signal,
        });
        setFirmwareFileId(newId);
        setState("ready");
      } catch (err) {
        if (controller.signal.aborted) return;
        setErrorMessage(err instanceof Error ? err.message : String(err));
        setState("error");
      }
    },
    [checkFirmwareFile, uploadFirmwareFile, serverConfig, getConfig],
  );

  const handleFileInputChange = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      const selected = e.target.files?.[0];
      if (selected) {
        void processFile(selected);
      }
    },
    [processFile],
  );

  const handleDragEnter = useCallback((e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragActive(true);
  }, []);

  const handleDragOver = useCallback((e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
  }, []);

  const handleDragLeave = useCallback((e: DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragActive(false);
  }, []);

  const handleDrop = useCallback(
    (e: DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      setIsDragActive(false);

      const droppedFile = e.dataTransfer.files[0];
      if (droppedFile) {
        void processFile(droppedFile);
      }
    },
    [processFile],
  );

  const handleDropZoneClick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleConfirm = useCallback(() => {
    if (firmwareFileId) {
      onConfirm(firmwareFileId);
      resetState();
    }
  }, [firmwareFileId, onConfirm, resetState]);

  const handleDismiss = useCallback(() => {
    abortControllerRef.current?.abort();
    abortControllerRef.current = null;
    resetState();
    onDismiss();
  }, [onDismiss, resetState]);

  const isProcessing = state === "hashing" || state === "checking" || state === "uploading";
  const configLoaded = serverConfig !== null;

  const buttons =
    state === "ready" ? [{ text: "Update firmware", variant: variants.primary, onClick: handleConfirm }] : undefined;

  return (
    <Modal
      open={open}
      contentHeader="Update firmware"
      onDismiss={handleDismiss}
      buttons={buttons}
      size="small"
      divider={false}
    >
      <div className="mt-6 flex flex-col gap-4">
        {state === "idle" && !configLoaded && (
          <div className="flex items-center justify-center p-8">
            <ProgressCircular indeterminate size={24} />
          </div>
        )}

        {state === "idle" && configLoaded && (
          <FileDropZone
            extensions={serverConfig.allowedExtensions}
            isDragActive={isDragActive}
            onClick={handleDropZoneClick}
            onDragEnter={handleDragEnter}
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
            onFileSelect={handleFileInputChange}
            fileInputRef={fileInputRef}
          />
        )}

        {isProcessing && file && (
          <FileProcessingStatus
            state={state as "hashing" | "checking" | "uploading"}
            fileName={file.name}
            fileSize={file.size}
            uploadProgress={uploadProgress}
          />
        )}

        {state === "ready" && file && <FileReadyStatus fileName={file.name} fileSize={file.size} />}

        {state === "error" && errorMessage && <FileErrorStatus message={errorMessage} onRetry={resetState} />}
      </div>
    </Modal>
  );
};

export default FirmwareUpdateModal;
