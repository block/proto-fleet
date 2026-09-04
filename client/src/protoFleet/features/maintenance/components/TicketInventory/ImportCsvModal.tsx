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
  const [bytes, setBytes] = useState<Uint8Array>();
  const [preview, setPreview] = useState<InventoryCsvPreview>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const select = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    const exact = new Uint8Array(await file.arrayBuffer());
    setBytes(exact);
    setLoading(true);
    setError(null);
    const value = await onPreview(exact);
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
  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Import inventory CSV"
      buttons={
        preview
          ? [
              {
                text: `Import ${preview.validCount} parts`,
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
        {!bytes ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border-2 border-dashed border-border-5 p-8">
            <span>Upload a CSV with part, site, quantity, and bin columns.</span>
            <input
              aria-label="Inventory CSV"
              ref={ref}
              type="file"
              accept=".csv,text/csv"
              onChange={(event) => void select(event)}
              className="hidden"
            />
            <Button
              text="Select file"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => ref.current?.click()}
            />
          </div>
        ) : loading && !preview ? (
          <span role="status">Parsing CSV…</span>
        ) : preview ? (
          <>
            {preview.errorCount ? (
              <Callout intent="warning" prefixIcon={<Alert width="w-4" />} title="Fix all errors before importing" />
            ) : null}
            <div className="max-h-80 overflow-auto">
              <table className="w-full text-300">
                <thead>
                  <tr>
                    <th>Row</th>
                    <th>Part name</th>
                    <th>Type</th>
                    <th>Manufacturer</th>
                    <th>Part number</th>
                    <th>Site</th>
                    <th>On hand</th>
                    <th>Reorder point</th>
                    <th>Bin</th>
                    <th>Error</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.rows.map((row) => (
                    <tr key={row.rowNumber} className={row.error ? "bg-intent-critical-fill/10" : ""}>
                      <td>{row.rowNumber}</td>
                      <td>{row.name}</td>
                      <td>{row.type}</td>
                      <td>{row.manufacturer}</td>
                      <td>{row.partNumber}</td>
                      <td>{row.siteName}</td>
                      <td>{row.onHand}</td>
                      <td>{row.reorderPoint}</td>
                      <td>{row.binLocation}</td>
                      <td>{row.error}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        ) : null}
        {error ? <div role="alert">{error}</div> : null}
      </div>
    </Modal>
  );
};
export default ImportCsvModal;
