import { useCallback, useEffect, useRef, useState } from "react";
import {
  type FrigatePackageUploadResult,
  useFrigatePackages,
  validateFrigatePackageFiles,
} from "@/protoFleet/api/useFrigatePackages";
import { FileProcessingStatus } from "@/protoFleet/components/FirmwareUpload";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal/Modal";
import ReleaseFilePicker from "@/shared/features/frigateUpdate/ReleaseFilePicker";
import {
  missingReleaseFiles,
  RELEASE_PACKAGE_FILE_KINDS,
  type ReleaseFileKind,
  type ReleaseFiles,
  type ReleasePackageFileKey,
} from "@/shared/features/frigateUpdate/releaseFiles";

type FileKey = ReleasePackageFileKey | "publicKey";
type ChosenFiles = ReleaseFiles<FileKey>;

// Fleet can check the signature with a public key from the upload when it has no release key installed.
const FILE_KINDS: ReleaseFileKind<FileKey>[] = [
  ...RELEASE_PACKAGE_FILE_KINDS,
  { key: "publicKey", extension: ".pem", label: "Public key", name: "public key", required: false },
];

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

  const choose = useCallback((next: ChosenFiles) => {
    setSelectionError(undefined);
    setError(undefined);
    setFiles(next);
  }, []);

  const { deb, statement, signature, publicKey } = files;
  const ready = missingReleaseFiles(FILE_KINDS, files).length === 0;

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
        title="Upload package"
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
      title="Upload package"
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
            <ReleaseFilePicker
              kinds={FILE_KINDS}
              files={files}
              onChange={choose}
              onReject={setSelectionError}
              emptyHint="Select the .deb, release statement and signature. Public key is optional."
              inputLabel="Package files"
            />
            <p className="text-200 text-text-primary-70">
              Keep the release file name of a ProtoOS package: proto-os_&lt;tag&gt;_all.deb.
            </p>
          </>
        )}
      </div>
    </Modal>
  );
};

export default FrigatePackageUploadDialog;
