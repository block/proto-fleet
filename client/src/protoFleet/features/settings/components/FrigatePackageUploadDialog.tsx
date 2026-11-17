import { useCallback, useEffect, useRef, useState } from "react";
import {
  type FrigatePackageUploadResult,
  useFrigatePackages,
  validateFrigatePackageFiles,
} from "@/protoFleet/api/useFrigatePackages";
import { FileProcessingStatus, FileSelectedStatus } from "@/protoFleet/components/FirmwareUpload";
import Button, { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal/Modal";

type FileKey = "deb" | "statement" | "signature";

const FIELDS: { key: FileKey; label: string; accept: string; hint: string }[] = [
  { key: "deb", label: "Package", accept: ".deb", hint: "The controller software .deb file." },
  {
    key: "statement",
    label: "Release statement",
    accept: ".json,application/json",
    hint: "The signed statement JSON.",
  },
  {
    key: "signature",
    label: "Release signature",
    accept: ".sig,.bin,application/octet-stream",
    hint: "The signature over the statement.",
  },
];

interface Props {
  open?: boolean;
  onSuccess: (result: FrigatePackageUploadResult) => void;
  onDismiss: () => void;
}

const FileField = ({
  label,
  accept,
  hint,
  file,
  disabled,
  onChange,
}: {
  label: string;
  accept: string;
  hint: string;
  file: File | undefined;
  disabled: boolean;
  onChange: (file: File | undefined) => void;
}) => {
  const input = useRef<HTMLInputElement>(null);
  return (
    <div className="flex flex-col gap-2">
      <div className="text-emphasis-300 text-text-primary">{label}</div>
      <input
        ref={input}
        type="file"
        aria-label={label}
        accept={accept}
        disabled={disabled}
        className="hidden"
        onChange={(event) => {
          onChange(event.target.files?.[0]);
          event.target.value = "";
        }}
      />
      {file ? (
        <FileSelectedStatus fileName={file.name} fileSize={file.size} onRemove={() => onChange(undefined)} />
      ) : (
        <div className="flex items-center justify-between gap-4 rounded-lg border border-dashed border-border-5 p-4">
          <span className="text-200 text-text-primary-70">{hint}</span>
          <Button
            variant={variants.secondary}
            text="Choose file"
            ariaLabel={`Choose ${label.toLowerCase()} file`}
            disabled={disabled}
            onClick={() => input.current?.click()}
          />
        </div>
      )}
    </div>
  );
};

const FrigatePackageUploadDialog = ({ open, onSuccess, onDismiss }: Props) => {
  const { uploadPackage } = useFrigatePackages();
  const [files, setFiles] = useState<Partial<Record<FileKey, File>>>({});
  const [progress, setProgress] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string>();
  const abort = useRef<AbortController | null>(null);

  useEffect(() => {
    if (open) return;
    abort.current?.abort();
    abort.current = null;
  }, [open]);
  useEffect(() => () => abort.current?.abort(), []);

  const reset = useCallback(() => {
    setFiles({});
    setProgress(0);
    setUploading(false);
    setError(undefined);
  }, []);

  const dismiss = useCallback(() => {
    abort.current?.abort();
    abort.current = null;
    reset();
    onDismiss();
  }, [onDismiss, reset]);

  const { deb, statement, signature } = files;
  const ready = !!deb && !!statement && !!signature;

  const upload = useCallback(async () => {
    if (!deb || !statement || !signature || uploading) return;
    const invalid = validateFrigatePackageFiles({ deb, statement, signature });
    if (invalid) {
      setError(invalid);
      return;
    }
    const controller = new AbortController();
    abort.current = controller;
    setError(undefined);
    setProgress(0);
    setUploading(true);
    try {
      const result = await uploadPackage({
        deb,
        statement,
        signature,
        onProgress: setProgress,
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      reset();
      onSuccess(result);
    } catch (cause) {
      if (controller.signal.aborted) return;
      setError(cause instanceof Error ? cause.message : String(cause));
      setUploading(false);
    }
  }, [deb, onSuccess, reset, signature, statement, uploadPackage, uploading]);

  return (
    <Modal
      open={open}
      title="Upload controller software"
      onDismiss={dismiss}
      divider={false}
      buttons={[
        {
          text: uploading ? "Uploading…" : error ? "Try again" : "Upload",
          variant: variants.primary,
          onClick: () => void upload(),
          dismissModalOnClick: false,
          disabled: !ready || uploading,
        },
      ]}
    >
      <div className="mt-6 flex flex-col gap-4">
        {FIELDS.map(({ key, ...field }) => (
          <FileField
            key={key}
            {...field}
            file={files[key]}
            disabled={uploading}
            onChange={(file) => {
              setError(undefined);
              setFiles((current) => ({ ...current, [key]: file }));
            }}
          />
        ))}
        {uploading && deb ? (
          <FileProcessingStatus state="uploading" fileName={deb.name} fileSize={deb.size} uploadProgress={progress} />
        ) : null}
        {error ? (
          <p role="alert" className="text-300 text-intent-critical-text">
            {error}
          </p>
        ) : null}
      </div>
    </Modal>
  );
};

export default FrigatePackageUploadDialog;
