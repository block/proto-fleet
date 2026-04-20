import NamePreview from "@/shared/components/NamePreview";
import ProgressCircular from "@/shared/components/ProgressCircular";

export interface PreviewRow {
  currentName: string;
  newName: string;
}

interface BulkRenamePreviewPanelProps {
  isLoadingPreview: boolean;
  previewRows: PreviewRow[];
  showPreviewEllipsis: boolean;
}

const BulkRenamePreviewPanel = ({
  isLoadingPreview,
  previewRows,
  showPreviewEllipsis,
}: BulkRenamePreviewPanelProps) => {
  const mobilePreviewRow = previewRows[0];

  return (
    <>
      <div
        className="flex min-h-16 items-center justify-center px-6 py-4 laptop:hidden desktop:hidden"
        data-testid="bulk-rename-mobile-preview"
      >
        {isLoadingPreview ? (
          <ProgressCircular indeterminate />
        ) : mobilePreviewRow ? (
          <div className="grid w-full grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-x-6">
            <NamePreview
              currentName={mobilePreviewRow.currentName}
              newName={mobilePreviewRow.newName}
              layout="inline"
            />
          </div>
        ) : (
          <div className="text-300 text-text-primary-50">No preview available</div>
        )}
      </div>

      <div
        className="hidden flex-col items-center justify-center gap-6 px-16 pt-6 pb-4 laptop:flex laptop:flex-1 desktop:flex desktop:flex-1"
        data-testid="bulk-rename-desktop-preview"
      >
        {isLoadingPreview ? (
          <div className="flex w-full items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        ) : previewRows.length === 0 ? (
          <div className="flex w-full items-center justify-center text-300 text-text-primary-50">
            No preview available
          </div>
        ) : (
          <div className="flex w-full justify-center">
            <div className="inline-grid max-w-full grid-cols-[fit-content(100%)_auto_fit-content(100%)] items-center gap-x-6 gap-y-6">
              {previewRows.slice(0, showPreviewEllipsis ? 3 : previewRows.length).map((row, index) => (
                <NamePreview
                  key={`${row.currentName}-${index}`}
                  currentName={row.currentName}
                  newName={row.newName}
                  layout="inline"
                />
              ))}

              {showPreviewEllipsis ? (
                <div className="col-span-3 px-2 text-center text-heading-200 text-text-primary-30">...</div>
              ) : null}

              {showPreviewEllipsis
                ? previewRows
                    .slice(-3)
                    .map((row, index) => (
                      <NamePreview
                        key={`${row.currentName}-tail-${index}`}
                        currentName={row.currentName}
                        newName={row.newName}
                        layout="inline"
                      />
                    ))
                : null}
            </div>
          </div>
        )}
      </div>
    </>
  );
};

export default BulkRenamePreviewPanel;
