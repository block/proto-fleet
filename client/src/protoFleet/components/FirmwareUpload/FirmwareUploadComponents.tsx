import type { ChangeEvent, DragEvent } from "react";
import { useCallback, useRef, useState } from "react";
import clsx from "clsx";
import { ArrowUp, Checkmark } from "@/shared/assets/icons";
import { formatFileSize } from "@/shared/components/FileSizeValue";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";

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
  onFileSelect: (file: File) => void;
  disabled?: boolean;
}

export function FileDropZone({ extensions, onFileSelect, disabled }: FileDropZoneProps) {
  const [isDragActive, setIsDragActive] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

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
      if (disabled) return;
      const droppedFile = e.dataTransfer.files[0];
      if (droppedFile) onFileSelect(droppedFile);
    },
    [disabled, onFileSelect],
  );

  const handleClick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileInputChange = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      const selected = e.target.files?.[0];
      if (selected) onFileSelect(selected);
      if (fileInputRef.current) fileInputRef.current.value = "";
    },
    [onFileSelect],
  );

  return (
    <>
      <button
        type="button"
        disabled={disabled}
        className={clsx(
          "flex cursor-pointer flex-col items-center justify-center gap-3 rounded-lg border-2 border-dashed p-8 transition-colors",
          disabled && "pointer-events-none opacity-50",
          isDragActive ? "border-border-focus bg-surface-elevated-base" : "border-border-5 hover:border-border-20",
        )}
        onClick={handleClick}
        onDragEnter={handleDragEnter}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        data-testid="firmware-drop-zone"
      >
        <ArrowUp className="text-text-secondary" />
        <div className="text-center">
          <div className="text-300 text-text-primary">Drag and drop a firmware file here, or click to browse</div>
          <div className="text-text-secondary mt-1 text-200">Accepted formats: {extensions.join(", ")}</div>
        </div>
      </button>
      <input
        ref={fileInputRef}
        type="file"
        accept={buildAcceptString(extensions)}
        onChange={handleFileInputChange}
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

export function FileProcessingStatus({ state, fileName, fileSize, uploadProgress }: FileProcessingStatusProps) {
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

export function FileReadyStatus({ fileName, fileSize }: FileReadyStatusProps) {
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

export function FileErrorStatus({ message, onRetry }: FileErrorStatusProps) {
  return (
    <div className="flex flex-col gap-3">
      <div className="text-300 text-intent-warning-fill">{message}</div>
      <button type="button" className="text-text-link cursor-pointer text-300 underline" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}
