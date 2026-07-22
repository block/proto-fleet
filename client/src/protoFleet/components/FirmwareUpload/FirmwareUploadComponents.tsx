import type { ChangeEvent, DragEvent } from "react";
import { useCallback, useRef, useState } from "react";
import clsx from "clsx";
import { Checkmark } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
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
    if (disabled) return;
    fileInputRef.current?.click();
  }, [disabled]);

  const handleFileInputChange = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      if (disabled) return;
      const selected = e.target.files?.[0];
      if (selected) onFileSelect(selected);
      if (fileInputRef.current) fileInputRef.current.value = "";
    },
    [disabled, onFileSelect],
  );

  const formattedExtensions =
    extensions.length <= 1
      ? extensions.join(", ")
      : `${extensions.slice(0, -1).join(", ")}, and ${extensions[extensions.length - 1]}`;

  return (
    <div className="flex flex-col gap-3">
      <div
        className={clsx(
          "flex cursor-pointer flex-col items-center justify-center gap-4 rounded-2xl bg-grayscale-gray-5 p-12 transition-colors",
          disabled && "pointer-events-none opacity-50",
          isDragActive && "ring-2 ring-border-primary",
        )}
        onClick={handleClick}
        onDragEnter={handleDragEnter}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        data-testid="firmware-drop-zone"
        role="button"
        aria-disabled={disabled}
        tabIndex={disabled ? -1 : 0}
      >
        <div className="text-300 text-text-primary">Drag update files here</div>
        <div className="text-200 text-text-primary-70">or</div>
        <Button
          variant={variants.secondary}
          disabled={disabled}
          onClick={(e) => {
            e.stopPropagation();
            handleClick();
          }}
          text="Choose file"
        />
      </div>
      <div className="text-200 text-text-primary-70">Supported file types: {formattedExtensions}</div>
      <input
        ref={fileInputRef}
        type="file"
        accept={buildAcceptString(extensions)}
        onChange={handleFileInputChange}
        disabled={disabled}
        className="hidden"
        data-testid="firmware-file-input"
      />
    </div>
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
        <div className="text-200 text-text-primary-70">
          {state === "hashing" ? "Computing checksum..." : null}
          {state === "checking" ? "Checking server..." : null}
          {state === "uploading" ? `${uploadProgress}% uploaded, ${formatFileSize(fileSize)}` : null}
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
        <div className="text-200 text-text-primary-70">{formatFileSize(fileSize)}, Ready</div>
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
      <Button
        variant={variants.textOnly}
        textColor="text-core-accent-fill"
        textOnlyUnderlineOnHover={false}
        className="w-fit"
        onClick={onRetry}
        text="Try again"
      />
    </div>
  );
}
