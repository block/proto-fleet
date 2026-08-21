import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import ImportCsvModal from "./ImportCsvModal";

const meta = {
  title: "Proto Fleet/Maintenance/Import inventory",
  component: ImportCsvModal,
  args: {
    onDismiss: action("dismiss inventory import"),
    onSuccess: action("confirm inventory import"),
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
