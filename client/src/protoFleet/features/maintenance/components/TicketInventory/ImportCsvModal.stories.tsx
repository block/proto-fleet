import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ImportCsvModal from "./ImportCsvModal";

const meta = {
  title: "Proto Fleet/Maintenance/Import inventory",
  component: ImportCsvModal,
  args: {
    onDismiss: action("dismiss inventory import"),
    onPreview: async (bytes) => {
      action("preview inventory import")(bytes);
      return { rows: [], validCount: 0, errorCount: 0 };
    },
    onConfirm: async (bytes) => {
      action("confirm inventory import")(bytes);
      return 0;
    },
    onSuccess: action("inventory imported"),
  },
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "Review the CSV drop zone and file-selection entry point for bulk parts inventory imports.",
      },
    },
  },
  tags: ["autodocs"],
} satisfies Meta<typeof ImportCsvModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const SelectCsv: Story = {};
