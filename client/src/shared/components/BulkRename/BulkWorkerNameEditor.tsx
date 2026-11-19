import { type ReactNode, useState } from "react";

import {
  type BulkRenamePreferences,
  type BulkRenamePreviewMiner,
  type BulkRenamePropertyId,
  type BulkRenamePropertyOptions,
  type BulkRenameSeparatorId,
  reorderBulkRenameProperties,
  updateBulkRenameProperty,
} from "./bulkRenameDefinitions";
import BulkRenameOptionModals from "./BulkRenameOptionModals";
import { buildBulkRenamePropertyPreview, findBulkRenamePropertyPreviewMinerIndex } from "./bulkRenamePreview";
import BulkRenamePreviewPanel, { type PreviewRow } from "./BulkRenamePreviewPanel";
import BulkRenamePropertyForm from "./BulkRenamePropertyForm";
import { Info } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import FullScreenTwoPaneModal from "@/shared/components/FullScreenTwoPaneModal";

interface BulkWorkerNameEditorProps {
  open?: boolean;
  preferences: BulkRenamePreferences;
  onPreferencesChange: (preferences: BulkRenamePreferences) => void;
  previewMiners: BulkRenamePreviewMiner[];
  previewRows: PreviewRow[];
  showPreviewEllipsis?: boolean;
  isLoadingPreview?: boolean;
  isBusy?: boolean;
  applyDisabled?: boolean;
  applyLabel: string;
  propertyLabels?: Partial<Record<BulkRenamePropertyId, string>>;
  separatorIds?: readonly BulkRenameSeparatorId[];
  feedback?: ReactNode;
  onApply: () => void;
  onDismiss: () => void;
}

export default function BulkWorkerNameEditor({
  open = true,
  preferences,
  onPreferencesChange,
  previewMiners,
  previewRows,
  showPreviewEllipsis = false,
  isLoadingPreview = false,
  isBusy = false,
  applyDisabled = false,
  applyLabel,
  propertyLabels,
  separatorIds,
  feedback,
  onApply,
  onDismiss,
}: BulkWorkerNameEditorProps) {
  const [options, setOptions] = useState<{ id: BulkRenamePropertyId; draft?: BulkRenamePropertyOptions }>();
  if (!open && options) setOptions(undefined);

  const property = preferences.properties.find(({ id }) => id === options?.id);
  let propertyPreview;
  if (property) {
    const previewPreferences = updateBulkRenameProperty(preferences, property.id, (current) => ({
      ...current,
      enabled: true,
      options: options?.draft ?? current.options,
    }));
    const index = findBulkRenamePropertyPreviewMinerIndex(previewPreferences, property.id, previewMiners);
    if (index !== null) {
      const miner = previewMiners[index];
      propertyPreview = buildBulkRenamePropertyPreview(previewPreferences, property.id, miner, miner.counterIndex);
    }
  }
  const closeOptions = () => setOptions(undefined);

  return (
    <>
      <FullScreenTwoPaneModal
        open={open}
        title="Update worker names"
        dialogLabel="Update worker names"
        onDismiss={onDismiss}
        isBusy={isBusy}
        closeAriaLabel="Close update worker names"
        buttons={[
          {
            text: applyLabel,
            variant: variants.primary,
            onClick: onApply,
            disabled: isBusy || isLoadingPreview || applyDisabled,
            testId: "bulk-worker-name-save-button",
          },
        ]}
        primaryPane={
          <BulkRenamePropertyForm
            preferences={preferences}
            propertiesTitle="Worker name properties"
            propertyLabels={propertyLabels}
            separatorIds={separatorIds}
            onDragEnd={({ active, over }) => {
              if (!over || active.id === over.id) return;
              onPreferencesChange(
                reorderBulkRenameProperties(
                  preferences,
                  active.id as BulkRenamePropertyId,
                  over.id as BulkRenamePropertyId,
                ),
              );
            }}
            onOpenOptions={(id) => setOptions({ id })}
            onToggleEnabled={(id, enabled) =>
              onPreferencesChange(updateBulkRenameProperty(preferences, id, (item) => ({ ...item, enabled })))
            }
            onChangeSeparator={(separator) => onPreferencesChange({ ...preferences, separator })}
            leadingContent={
              <>
                <Callout
                  className="mb-3"
                  intent="default"
                  prefixIcon={<Info />}
                  title="Worker names determine how miners appear in pool dashboards."
                />
                {feedback}
              </>
            }
          />
        }
        secondaryPane={
          <BulkRenamePreviewPanel
            isLoadingPreview={isLoadingPreview}
            previewRows={previewRows}
            showPreviewEllipsis={showPreviewEllipsis}
          />
        }
      />
      <BulkRenameOptionModals
        activeOptionsPropertyId={open ? (property?.id ?? null) : null}
        activeOptionsPropertyOptions={property?.options ?? null}
        previewName={propertyPreview?.previewName ?? ""}
        highlightedText={propertyPreview?.highlightedText}
        highlightStartIndex={propertyPreview?.highlightStartIndex}
        onDismiss={closeOptions}
        onChange={(draft) => setOptions((current) => current && { ...current, draft: draft ?? undefined })}
        onConfirm={(id, options) => {
          onPreferencesChange(updateBulkRenameProperty(preferences, id, (item) => ({ ...item, options })));
          closeOptions();
        }}
      />
    </>
  );
}
