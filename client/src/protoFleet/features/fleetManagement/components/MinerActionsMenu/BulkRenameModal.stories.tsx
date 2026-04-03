import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import { createDefaultBulkRenamePreferences } from "./bulkRenameDefinitions";
import BulkRenamePreviewPanel, { type PreviewRow } from "./BulkRenamePreviewPanel";
import BulkRenamePropertyForm from "./BulkRenamePropertyForm";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { variants } from "@/shared/components/Button";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

const samplePreviewRows: PreviewRow[] = [
  { currentName: "miner-001", newName: "site-a-rack-1-001" },
  { currentName: "miner-002", newName: "site-a-rack-1-002" },
  { currentName: "miner-003", newName: "site-a-rack-1-003" },
  { currentName: "miner-004", newName: "site-a-rack-2-001" },
  { currentName: "miner-005", newName: "site-a-rack-2-002" },
  { currentName: "miner-006", newName: "site-a-rack-2-003" },
];

type BulkRenameModalStoryProps = {
  infoMessage: string;
  isLoadingPreview?: boolean;
  showPreviewEllipsis?: boolean;
  minerCount?: number;
};

const BulkRenameModalStory = ({
  infoMessage,
  isLoadingPreview = false,
  showPreviewEllipsis = false,
  minerCount = 6,
}: BulkRenameModalStoryProps) => {
  const [open, setOpen] = useState(true);
  const [preferences, setPreferences] = useState(createDefaultBulkRenamePreferences);

  if (!open) {
    return (
      <div className="flex h-screen items-center justify-center bg-surface-base">
        <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-surface-base p-4">
      <div className="mb-4 max-w-3xl rounded-lg bg-intent-info-10 p-4 text-300 text-text-primary">{infoMessage}</div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <FullScreenTwoPaneModal
        open
        title="Rename miners"
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        buttons={[
          {
            text: `Apply to ${minerCount} miners`,
            variant: variants.primary,
            onClick: action("apply"),
          },
        ]}
        primaryPane={
          <BulkRenamePreviewPanel
            isLoadingPreview={isLoadingPreview}
            previewRows={samplePreviewRows}
            showPreviewEllipsis={showPreviewEllipsis}
          />
        }
        secondaryPane={
          <BulkRenamePropertyForm
            preferences={preferences}
            onDragEnd={action("dragEnd")}
            onOpenOptions={action("openOptions")}
            onToggleEnabled={(propertyId, enabled) => {
              action("toggleEnabled")(propertyId, enabled);
              setPreferences((current) => ({
                ...current,
                properties: current.properties.map((p) => (p.id === propertyId ? { ...p, enabled } : p)),
              }));
            }}
            onChangeSeparator={(separator) => {
              action("changeSeparator")(separator);
              setPreferences((current) => ({ ...current, separator }));
            }}
          />
        }
      />
    </div>
  );
};

const meta = {
  title: "Proto Fleet/Fleet Management/Bulk Rename/BulkRenameModal",
  component: BulkRenameModalStory,
  parameters: {
    layout: "fullscreen",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof BulkRenameModalStory>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    infoMessage: "Bulk rename modal with sample preview data and interactive property form.",
    minerCount: 6,
  },
};

export const LoadingPreview: Story = {
  args: {
    infoMessage: "Bulk rename modal showing the loading state for the preview panel.",
    isLoadingPreview: true,
  },
};

export const WithEllipsis: Story = {
  args: {
    infoMessage: "Bulk rename modal with ellipsis indicating more miners beyond the visible preview sample.",
    showPreviewEllipsis: true,
    minerCount: 150,
  },
};
