import { useCallback, useEffect, useRef, useState } from "react";
import {
  type FrigatePackageUploadResult,
  useFrigatePackages,
  validateFrigatePackageFiles,
} from "@/protoFleet/api/useFrigatePackages";
import { FileProcessingStatus, FileSelectedStatus } from "@/protoFleet/components/FirmwareUpload";
import { Alert } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal/Modal";

type FileKey = "deb" | "statement" | "signature" | "publicKey";
type ChosenFiles = Partial<Record<FileKey, File>>;

// A release is one file of each kind, told apart by its extension. `name` is the kind in a sentence.
const FILE_KINDS: { key: FileKey; extension: string; label: string; name: string; required: boolean }[] = [
  { key: "deb", extension: ".deb", label: "Debian package (.deb)", name: "package", required: true },
  { key: "statement", extension: ".json", label: "Release statement", name: "release statement", required: true },
  { key: "signature", extension: ".sig", label: "Release signature", name: "release signature", required: true },
  { key: "publicKey", extension: ".pem", label: "Public key", name: "public key", required: false },
];
const ACCEPT = FILE_KINDS.map((kind) => kind.extension).join(",");

/** Sorts one selection by kind, or says why it can't be used. */
function sortByKind(selected: File[]): { files: ChosenFiles } | { error: string } {
  const files: ChosenFiles = {};
  for (const file of selected) {
    const kind = FILE_KINDS.find(({ extension }) => file.name.toLowerCase().endsWith(extension));
    if (!kind) return { error: `${file.name} is not a supported file type.` };
    if (files[kind.key]) return { error: `Select only one ${kind.name}.` };
    files[kind.key] = file;
  }
  return { files };
}

interface Props {
  open?: boolean;
  onSuccess: (result: FrigatePackageUploadResult) => void;
  onDismiss: () => void;
}

const FrigatePackageUploadDialog = ({ open, onSuccess, onDismiss }: Props) => {
  const { uploadPackage } = useFrigatePackages();
  const [files, setFiles] = useState<ChosenFiles>({});
  const [progress, setProgress] = useState(0);
  const [uploading, setUploading] = useState(false);
  // A failed upload, which the operator can try again.
  const [error, setError] = useState<string>();
  // A selection that can't be used. It is not a failed upload, so it doesn't offer "Try again".
  const [selectionError, setSelectionError] = useState<string>();
  // An upload that Fleet stored without verifying the release signature. The
  // dialog stays open to say so before it reports the result.
  const [unverified, setUnverified] = useState<FrigatePackageUploadResult>();
  const abort = useRef<AbortController | null>(null);
  const input = useRef<HTMLInputElement>(null);

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
    setSelectionError(undefined);
    setUnverified(undefined);
  }, []);

  const dismiss = useCallback(() => {
    abort.current?.abort();
    abort.current = null;
    reset();
    // The package is already stored, so closing the warning still reports it.
    if (unverified) onSuccess(unverified);
    else onDismiss();
  }, [onDismiss, onSuccess, reset, unverified]);

  // A new selection adds to the chosen files and replaces a file of the same
  // kind, so a file that was left out can be added without choosing all again.
  const choose = useCallback((selected: File[]) => {
    if (selected.length === 0) return;
    const sorted = sortByKind(selected);
    if ("error" in sorted) {
      setSelectionError(sorted.error);
      return;
    }
    setSelectionError(undefined);
    setError(undefined);
    setFiles((current) => ({ ...current, ...sorted.files }));
  }, []);

  const { deb, statement, signature, publicKey } = files;
  const chosen = FILE_KINDS.filter((kind) => files[kind.key]);
  const missing = FILE_KINDS.filter((kind) => kind.required && !files[kind.key]).map((kind) => kind.name);
  const ready = missing.length === 0;
  let hint = "Select the .deb, release statement and signature. Public key is optional.";
  if (chosen.length > 0) hint = ready ? "All required files selected." : `Still needed: ${missing.join(", ")}.`;

  const upload = useCallback(async () => {
    if (!deb || !statement || !signature || uploading) return;
    const invalid = validateFrigatePackageFiles({ deb, statement, signature, publicKey });
    if (invalid) {
      setError(invalid);
      return;
    }
    const controller = new AbortController();
    abort.current = controller;
    setError(undefined);
    setSelectionError(undefined);
    setProgress(0);
    setUploading(true);
    try {
      const result = await uploadPackage({
        deb,
        statement,
        signature,
        publicKey,
        onProgress: setProgress,
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      if (result.warning) {
        setUploading(false);
        setUnverified(result);
        return;
      }
      reset();
      onSuccess(result);
    } catch (cause) {
      if (controller.signal.aborted) return;
      setError(cause instanceof Error ? cause.message : String(cause));
      setUploading(false);
    }
  }, [deb, onSuccess, publicKey, reset, signature, statement, uploadPackage, uploading]);

  if (unverified) {
    return (
      <Modal
        open={open}
        title="Upload controller software"
        onDismiss={dismiss}
        divider={false}
        buttons={[{ text: "Done", variant: variants.primary, onClick: dismiss, dismissModalOnClick: false }]}
      >
        <div className="mt-6">
          <Callout
            intent="warning"
            prefixIcon={<Alert />}
            title="Signature not verified"
            subtitle={unverified.warning}
            testId="frigate-upload-unverified"
          />
        </div>
      </Modal>
    );
  }

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
        {/* Above the picker, so a long file list can't push it out of view. */}
        {(selectionError ?? error) ? (
          <div role="alert">
            <Callout
              intent="danger"
              prefixIcon={<Alert />}
              title={selectionError ?? error}
              testId="frigate-upload-error"
            />
          </div>
        ) : null}
        {uploading && deb ? (
          <FileProcessingStatus state="uploading" fileName={deb.name} fileSize={deb.size} uploadProgress={progress} />
        ) : (
          <>
            <div className="flex items-center justify-between gap-4 rounded-lg border border-dashed border-border-5 p-4">
              <span role="status" className="text-200 text-text-primary-70">
                {hint}
              </span>
              <Button
                variant={variants.secondary}
                text="Choose files"
                className="shrink-0"
                onClick={() => input.current?.click()}
              />
            </div>
            <input
              ref={input}
              type="file"
              multiple
              accept={ACCEPT}
              aria-label="Controller software files"
              className="hidden"
              onChange={(event) => {
                choose(Array.from(event.target.files ?? []));
                event.target.value = "";
              }}
            />
            {chosen.map(({ key, label }) => {
              const file = files[key];
              return file ? (
                <div key={key} className="flex flex-col gap-2">
                  <div className="text-emphasis-300 text-text-primary">{label}</div>
                  <FileSelectedStatus
                    fileName={file.name}
                    fileSize={file.size}
                    onRemove={() => {
                      setError(undefined);
                      setSelectionError(undefined);
                      setFiles((current) => ({ ...current, [key]: undefined }));
                    }}
                  />
                </div>
              ) : null;
            })}
          </>
        )}
      </div>
    </Modal>
  );
};

export default FrigatePackageUploadDialog;
