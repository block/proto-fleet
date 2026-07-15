import { ChangeEvent, useCallback, useRef, useState } from "react";
import {
  ImportOperation,
  type ImportSiteMapCsvResponse,
  OmissionMode,
} from "@/protoFleet/api/generated/sitemap/v1/sitemap_pb";
import useSiteMapCsv from "@/protoFleet/api/useSiteMapCsv";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

interface SiteMapCsvImportModalProps {
  open?: boolean;
  onDismiss: () => void;
  onImported?: () => void;
}

const operationLabel = (operation: ImportOperation) => {
  switch (operation) {
    case ImportOperation.CREATE:
      return "Create";
    case ImportOperation.UPDATE:
      return "Update";
    case ImportOperation.DELETE:
      return "Delete";
    case ImportOperation.UNASSIGN:
      return "Unassign";
    case ImportOperation.MOVE:
      return "Move";
    case ImportOperation.RENAME:
      return "Rename";
    case ImportOperation.UNSPECIFIED:
    default:
      return "Change";
  }
};

const hasValidationErrors = (preview?: ImportSiteMapCsvResponse) => (preview?.errors.length ?? 0) > 0;

const SiteMapCsvImportModal = ({ open, onDismiss, onImported }: SiteMapCsvImportModalProps) => {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | undefined>();
  const [preview, setPreview] = useState<ImportSiteMapCsvResponse | undefined>();
  const [selectedOmissionMode, setSelectedOmissionMode] = useState<OmissionMode | undefined>();
  const [errorMessage, setErrorMessage] = useState<string | undefined>();
  const { importSiteMapCsv, isImportingSiteMapCsv } = useSiteMapCsv();

  const runPreview = useCallback(
    async (nextFile: File, omissionMode: OmissionMode) => {
      setErrorMessage(undefined);
      try {
        const response = await importSiteMapCsv({
          file: nextFile,
          omissionMode,
          dryRun: true,
        });
        setPreview(response);
      } catch (error) {
        setPreview(undefined);
        setErrorMessage(error instanceof Error ? error.message : "Failed to preview site map import.");
      }
    },
    [importSiteMapCsv],
  );

  const handleChooseFile = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileChange = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const nextFile = event.target.files?.[0];
      if (!nextFile) {
        return;
      }

      setFile(nextFile);
      setPreview(undefined);
      setSelectedOmissionMode(undefined);
      void runPreview(nextFile, OmissionMode.UNSPECIFIED);
      event.target.value = "";
    },
    [runPreview],
  );

  const handleSelectOmissionMode = useCallback(
    (nextMode: OmissionMode) => {
      if (!file) {
        return;
      }

      setSelectedOmissionMode(nextMode);
      void runPreview(file, nextMode);
    },
    [file, runPreview],
  );

  const handleCommit = useCallback(async () => {
    if (!file || !preview?.commitToken || preview.omissionChoiceRequired || hasValidationErrors(preview)) {
      return;
    }

    setErrorMessage(undefined);
    try {
      await importSiteMapCsv({
        file,
        omissionMode: selectedOmissionMode ?? OmissionMode.UNSPECIFIED,
        dryRun: false,
        commitToken: preview.commitToken,
      });
      pushToast({
        status: TOAST_STATUSES.success,
        message: "Site map import completed.",
      });
      onImported?.();
      onDismiss();
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Failed to import site map.");
    }
  }, [file, importSiteMapCsv, onDismiss, onImported, preview, selectedOmissionMode]);

  const omissionCounts = preview?.omissionCounts;
  const canCommit = Boolean(preview?.commitToken) && !preview?.omissionChoiceRequired && !hasValidationErrors(preview);

  return (
    <Modal
      open={open}
      title="Import site map CSV"
      description="Preview changes before applying a site, building, rack, and miner placement CSV."
      onDismiss={onDismiss}
      buttonSize={sizes.compact}
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onDismiss,
        },
        {
          text: "Confirm import",
          variant: variants.primary,
          onClick: handleCommit,
          disabled: !canCommit,
          loading: isImportingSiteMapCsv && Boolean(preview?.commitToken),
          dismissModalOnClick: false,
          testId: "confirm-site-map-import-button",
        },
      ]}
      testId="site-map-csv-import-modal"
    >
      <div className="flex flex-col gap-5">
        <div className="rounded-lg border border-border-5 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="min-w-0">
              <div className="text-emphasis-300 text-text-primary">{file?.name ?? "No file selected"}</div>
              <div className="text-200 text-text-primary-70">Supported file type: CSV</div>
            </div>
            <Button
              text={file ? "Choose another" : "Choose file"}
              variant={variants.secondary}
              size={sizes.compact}
              onClick={handleChooseFile}
              disabled={isImportingSiteMapCsv}
              testId="choose-site-map-csv-button"
            />
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept=".csv,text/csv"
            onChange={handleFileChange}
            className="hidden"
            data-testid="site-map-csv-file-input"
          />
        </div>

        {isImportingSiteMapCsv && !preview ? (
          <div className="text-300 text-text-primary-70">Checking CSV...</div>
        ) : null}

        {errorMessage ? (
          <div className="rounded-lg bg-intent-critical-10 p-4 text-300 text-text-critical">{errorMessage}</div>
        ) : null}

        {preview?.omissionChoiceRequired && omissionCounts ? (
          <div className="flex flex-col gap-3 rounded-lg border border-border-5 p-4">
            <div>
              <div className="text-emphasis-300 text-text-primary">Choose how omitted rows are handled</div>
              <div className="text-200 text-text-primary-70">
                The CSV omits {omissionCounts.sites} sites, {omissionCounts.buildings} buildings,
                {` ${omissionCounts.racks} racks, and ${omissionCounts.miners} miners.`}
              </div>
            </div>
            <div className="flex flex-col gap-3">
              <label className="flex cursor-pointer gap-3 rounded-lg border border-border-5 p-3">
                <Radio
                  name="site-map-omission-mode"
                  value={OmissionMode.LEAVE_IN_PLACE}
                  selected={selectedOmissionMode === OmissionMode.LEAVE_IN_PLACE}
                  disabled={isImportingSiteMapCsv}
                  onChange={() => handleSelectOmissionMode(OmissionMode.LEAVE_IN_PLACE)}
                />
                <div>
                  <div className="text-300 text-text-primary">Leave omitted rows in place</div>
                  <div className="text-200 text-text-primary-70">
                    Keep existing entities that are missing from the CSV.
                  </div>
                </div>
              </label>
              <label className="flex cursor-pointer gap-3 rounded-lg border border-border-5 p-3">
                <Radio
                  name="site-map-omission-mode"
                  value={OmissionMode.REMOVE_OMITTED}
                  selected={selectedOmissionMode === OmissionMode.REMOVE_OMITTED}
                  disabled={isImportingSiteMapCsv}
                  onChange={() => handleSelectOmissionMode(OmissionMode.REMOVE_OMITTED)}
                />
                <div>
                  <div className="text-300 text-text-primary">Remove omitted rows</div>
                  <div className="text-200 text-text-primary-70">
                    Treat missing rows as deletions or unassignments in the preview.
                  </div>
                </div>
              </label>
            </div>
          </div>
        ) : null}

        {preview?.warnings.length ? (
          <div className="flex flex-col gap-2 rounded-lg bg-core-primary-5 p-4">
            <div className="text-emphasis-300 text-text-primary">Warnings</div>
            {preview.warnings.map((warning) => (
              <div key={warning} className="text-200 text-text-primary-70">
                {warning}
              </div>
            ))}
          </div>
        ) : null}

        {preview?.errors.length ? (
          <div className="flex max-h-64 flex-col gap-2 overflow-auto rounded-lg border border-intent-critical-fill p-4">
            <div className="text-emphasis-300 text-text-critical">Fix these CSV errors</div>
            {preview.errors.map((error, index) => (
              <div key={`${error.section}-${error.row}-${index}`} className="text-200 text-text-primary-70">
                Row {error.row} {error.section ? `(${error.section})` : null}: {error.message}
              </div>
            ))}
          </div>
        ) : null}

        {preview && !preview.omissionChoiceRequired && !preview.errors.length ? (
          <div className="flex flex-col gap-3">
            <div className="text-emphasis-300 text-text-primary">Preview</div>
            {preview.changes.length ? (
              <div className="flex max-h-72 flex-col overflow-auto rounded-lg border border-border-5">
                {preview.changes.map((change, index) => (
                  <div
                    key={`${change.operation}-${change.entityType}-${change.description}-${index}`}
                    className="flex items-start justify-between gap-4 border-b border-border-5 px-4 py-3 last:border-b-0"
                  >
                    <div className="min-w-0">
                      <div className="text-300 text-text-primary">
                        {operationLabel(change.operation)} {change.entityType}
                      </div>
                      <div className="text-200 text-text-primary-70">{change.description}</div>
                    </div>
                    <div className="text-emphasis-300 text-text-primary">{change.count}</div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="rounded-lg border border-border-5 p-4 text-300 text-text-primary-70">
                No site map changes detected.
              </div>
            )}
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

export default SiteMapCsvImportModal;
