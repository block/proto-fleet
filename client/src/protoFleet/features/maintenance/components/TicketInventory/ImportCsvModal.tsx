import { useCallback, useRef, useState } from "react";

import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import { Alert } from "@/shared/assets/icons";
import Modal from "@/shared/components/Modal";

interface ImportCsvModalProps {
  onDismiss: () => void;
  onSuccess: () => void;
}

interface PreviewRow {
  name: string;
  type: string;
  siteName: string;
  onHand: number;
  reorderPoint: number;
  binLocation: string;
  error: string;
}

const ImportCsvModal = ({ onDismiss, onSuccess }: ImportCsvModalProps) => {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [previewRows, setPreviewRows] = useState<PreviewRow[]>([]);
  const [errorCount, setErrorCount] = useState(0);
  const [hasFile, setHasFile] = useState(false);

  const handleFileSelect = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setHasFile(true);
    // TODO: wire to ImportInventoryCsv RPC for server-side parsing
    setPreviewRows([]);
    setErrorCount(0);
  }, []);

  const handleConfirm = useCallback(() => {
    // TODO: wire to ConfirmInventoryImport RPC
    onSuccess();
  }, [onSuccess]);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Import inventory CSV"
      buttons={
        hasFile && previewRows.length > 0
          ? [
              {
                text: `Import ${previewRows.length - errorCount} parts`,
                variant: variants.primary,
                onClick: handleConfirm,
                disabled: previewRows.length === errorCount,
                dismissModalOnClick: false,
              },
            ]
          : undefined
      }
    >
      <div className="flex flex-col gap-4">
        {!hasFile ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border-2 border-dashed border-border-5 p-8">
            <span className="text-300 text-text-primary-70">
              Upload a CSV with columns: Part Name, Type, Site, On Hand, Reorder Point, Bin Location
            </span>
            <input
              ref={fileInputRef}
              type="file"
              accept=".csv"
              onChange={handleFileSelect}
              className="hidden"
            />
            <Button
              text="Select file"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => fileInputRef.current?.click()}
            />
          </div>
        ) : previewRows.length === 0 ? (
          <span className="text-300 text-text-primary-70">Parsing CSV...</span>
        ) : (
          <>
            {errorCount > 0 && (
              <Callout
                intent="warning"
                prefixIcon={<Alert width="w-4" />}
                title={`${errorCount} row${errorCount > 1 ? "s" : ""} have errors and will be skipped`}
              />
            )}
            <div className="max-h-80 overflow-auto">
              <table className="w-full text-300">
                <thead>
                  <tr className="border-b border-border-5 text-left text-text-primary-70">
                    <th className="p-2">Part Name</th>
                    <th className="p-2">Type</th>
                    <th className="p-2">Site</th>
                    <th className="p-2">On Hand</th>
                    <th className="p-2">Reorder Pt</th>
                    <th className="p-2">Bin</th>
                  </tr>
                </thead>
                <tbody>
                  {previewRows.map((row, i) => (
                    <tr
                      key={i}
                      className={`border-b border-border-5 ${row.error ? "bg-intent-critical-fill/10" : ""}`}
                    >
                      <td className="p-2">{row.name}</td>
                      <td className="p-2">{row.type}</td>
                      <td className="p-2">{row.siteName}</td>
                      <td className="p-2">{row.onHand}</td>
                      <td className="p-2">{row.reorderPoint}</td>
                      <td className="p-2">{row.binLocation}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
};

export default ImportCsvModal;
