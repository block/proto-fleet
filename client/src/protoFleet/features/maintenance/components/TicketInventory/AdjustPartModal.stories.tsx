import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";

import { mockInventoryParts } from "../../maintenance.stories.fixtures";
import AdjustPartModal from "./AdjustPartModal";

const meta = {
  title: "Proto Fleet/Maintenance/Adjust inventory",
  component: AdjustPartModal,
  args: {
    onDismiss: action("dismiss inventory adjustment"),
    onSubmit: async (value) => {
      action("save inventory adjustment")(value);
      return true;
    },
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
    part: {
      ...mockInventoryParts[1],
      manufacturer: "Bitmain",
      partNumber: "HS-S21",
      siteId: "1",
      available: mockInventoryParts[1].onHand - mockInventoryParts[1].allocated,
      lowStock: true,
      createdAt: null,
      updatedAt: null,
    },
  },
};

export const WellStockedConsumable: Story = {
  args: {
    part: {
      ...mockInventoryParts[4],
      manufacturer: "Generic",
      partNumber: "TP-1",
      siteId: "1",
      available: mockInventoryParts[4].onHand - mockInventoryParts[4].allocated,
      lowStock: false,
      createdAt: null,
      updatedAt: null,
    },
  },
};
