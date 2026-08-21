import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import { mockInventoryParts } from "../../mockData";
import AdjustPartModal from "./AdjustPartModal";

const meta = {
  title: "Proto Fleet/Maintenance/Adjust inventory",
  component: AdjustPartModal,
  args: {
    onDismiss: action("dismiss inventory adjustment"),
    onSuccess: action("save inventory adjustment"),
  },
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component: "Review stock count, reorder point, bin location, adjustment reason, and audit-note inputs.",
      },
    },
  },
  tags: ["autodocs"],
} satisfies Meta<typeof AdjustPartModal>;

export default meta;
type Story = StoryObj<typeof meta>;

export const LowStockHashboard: Story = {
  args: {
    part: mockInventoryParts[1],
  },
};

export const WellStockedConsumable: Story = {
  args: {
    part: mockInventoryParts[4],
  },
};
