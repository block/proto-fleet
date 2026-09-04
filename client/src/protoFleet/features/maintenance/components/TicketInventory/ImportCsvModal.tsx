import { type ChangeEvent, useRef, useState } from "react";
import type { InventoryCsvPreview } from "@/protoFleet/api/inventory";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";

interface Props {
  onDismiss: () => void;
  onPreview: (bytes: Uint8Array) => Promise<InventoryCsvPreview | null>;
  onConfirm: (bytes: Uint8Array) => Promise<number | null>;
  onSuccess: (count: number) => void;
}

const ImportCsvModal = ({ onDismiss, onPreview, onConfirm, onSuccess }: Props) => {
  const ref = useRef<HTMLInputElement>(null);
  const previewSequence = useRef(0);
  const [filename, setFilename] = useState("");
  const [bytes, setBytes] = useState<Uint8Array>();
  const [preview, setPreview] = useState<InventoryCsvPreview>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const select = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    event.target.value = "";
    const request = ++previewSequence.current;
    const exact = new Uint8Array(await file.arrayBuffer());
    if (request !== previewSequence.current) return;
    setFilename(file.name);
    setBytes(exact);
    setPreview(undefined);
    setLoading(true);
    setError(null);
    const value = await onPreview(exact);
    if (request !== previewSequence.current) return;
    setLoading(false);
    if (value) setPreview(value);
    else setError("Unable to preview CSV");
  };

  const confirm = async () => {
    if (!bytes || !preview || preview.errorCount) return;
    setLoading(true);
    const count = await onConfirm(bytes);
    setLoading(false);
    if (count === null) setError("Unable to import CSV");
    else onSuccess(count);
  };

  const validLabel = preview
    ? `${preview.validCount} ${preview.validCount === 1 ? "part" : "parts"} ready to import`
    : "";
  const invalidRows = preview?.rows.filter((row) => row.error) ?? [];

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Import inventory CSV"
      buttons={
        preview
          ? [
              {
                text: "Cancel",
                variant: variants.secondary,
                onClick: onDismiss,
              },
              {
                text: `Import ${preview.validCount} ${preview.validCount === 1 ? "part" : "parts"}`,
                variant: variants.primary,
                onClick: () => void confirm(),
                disabled: preview.errorCount > 0,
                loading,
                dismissModalOnClick: false,
              },
            ]
          : undefined
      }
    >
      <div className="flex flex-col gap-4">
        <input
          aria-label="Inventory CSV"
          ref={ref}
          type="file"
          accept=".csv,text/csv"
          onChange={(event) => void select(event)}
          className="hidden"
        />

        {!bytes ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border-2 border-dashed border-border-5 p-8">
            <span>Upload a CSV with part, site, quantity, and bin columns.</span>
            <Button
              text="Select file"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => ref.current?.click()}
            />
          </div>
        ) : (
          <div className="flex items-center justify-between gap-4 rounded-xl bg-surface-5 px-4 py-3">
            <span className="min-w-0 truncate font-medium">{filename}</span>
            <Button
              text="Choose another"
              variant={variants.ghost}
              size={buttonSizes.compact}
              onClick={() => ref.current?.click()}
            />
          </div>
        )}

        {loading && !preview ? <span role="status">Parsing CSV…</span> : null}
        {preview ? (
          <>
            <div className="flex gap-6">
              <span>{validLabel}</span>
              {preview.errorCount ? (
                <span>
                  {preview.errorCount} {preview.errorCount === 1 ? "row needs" : "rows need"} attention
                </span>
              ) : null}
            </div>
            {preview.errorCount ? (
              <Callout intent="warning" prefixIcon={<Alert width="w-4" />} title="Fix all errors before importing" />
            ) : null}
            {invalidRows.length ? (
              <ul className="max-h-64 space-y-2 overflow-auto rounded-xl bg-surface-5 p-4">
                {invalidRows.map((row) => (
                  <li key={row.rowNumber}>
                    Row {row.rowNumber}: {row.error}
                  </li>
                ))}
              </ul>
            ) : null}
          </>
        ) : null}
        {error ? <div role="alert">{error}</div> : null}
      </div>
    </Modal>
  );
};

export default ImportCsvModal;
