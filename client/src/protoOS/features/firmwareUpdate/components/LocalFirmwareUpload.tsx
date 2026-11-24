import { useRef, useState } from "react";

import Button from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import FileSizeValue from "@/shared/components/FileSizeValue";

interface LocalFirmwareUploadProps {
  disabled?: boolean;
  pending: boolean;
  error?: string;
  onClearError?: () => void;
  onUpload: (file: File) => Promise<boolean>;
}

const LocalFirmwareUpload = ({ disabled, pending, error, onClearError, onUpload }: LocalFirmwareUploadProps) => {
  const [open, setOpen] = useState(false);
  const [file, setFile] = useState<File>();
  const [validationError, setValidationError] = useState<string>();
  const inputRef = useRef<HTMLInputElement>(null);
  const close = () => {
    if (pending) return;
    setOpen(false);
    setFile(undefined);
    setValidationError(undefined);
    onClearError?.();
  };

  return (
    <>
      <Button variant="secondary" size="compact" disabled={disabled || pending} onClick={() => setOpen(true)}>
        Upload firmware
      </Button>
      {open ? (
        <Dialog
          title="Upload firmware"
          manageFocus
          onDismiss={close}
          buttons={[
            { text: "Cancel", variant: "secondary", disabled: pending, onClick: close },
            {
              text: pending ? "Uploading…" : "Upload and install",
              variant: "primary",
              disabled: !file || disabled || pending,
              loading: pending,
              onClick: async () => {
                if (file && (await onUpload(file))) {
                  setOpen(false);
                  setFile(undefined);
                }
              },
            },
          ]}
        >
          <div className="space-y-4 text-300">
            <p>Choose an R2 .swu file. Installation starts when the upload finishes. Keep the rig powered on.</p>
            <Button variant="secondary" disabled={disabled || pending} onClick={() => inputRef.current?.click()}>
              {file ? "Change file" : "Choose file"}
            </Button>
            <input
              ref={inputRef}
              type="file"
              accept=".swu"
              aria-label="Firmware file"
              className="hidden"
              disabled={disabled || pending}
              onChange={(event) => {
                const selected = event.target.files?.[0];
                event.target.value = "";
                if (!selected) return;
                onClearError?.();
                if (!selected.name.toLowerCase().endsWith(".swu") || selected.size === 0) {
                  setFile(undefined);
                  setValidationError("Choose a non-empty .swu firmware file.");
                  return;
                }
                setValidationError(undefined);
                setFile(selected);
              }}
            />
            {file ? (
              <div className="break-all">
                <div>{file.name}</div>
                <div className="text-text-primary-70">
                  <FileSizeValue value={file.size} />
                </div>
              </div>
            ) : null}
            {pending ? <p role="status">Uploading firmware. Keep this page open until the upload finishes.</p> : null}
            {validationError || error ? (
              <p role="alert" className="text-text-critical">
                {validationError || error}
              </p>
            ) : null}
          </div>
        </Dialog>
      ) : null}
    </>
  );
};

export default LocalFirmwareUpload;
