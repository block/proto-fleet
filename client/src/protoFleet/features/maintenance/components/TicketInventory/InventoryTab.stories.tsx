import type { Meta, StoryObj } from "@storybook/react";

import MaintenanceOptionsProvider from "../MaintenanceOptionsProvider";
import InventoryTab from "./InventoryTab";

const meta = {
  title: "Proto Fleet/Maintenance/Parts inventory",
  component: InventoryTab,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Review inventory totals, low-stock emphasis, site and type filters, CSV actions, and per-part adjustment entry points.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-6 laptop:p-10">
        <MaintenanceOptionsProvider>
          <Story />
        </MaintenanceOptionsProvider>
      </div>
    ),
  ],
  tags: ["autodocs"],
} satisfies Meta<typeof InventoryTab>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Overview: Story = {};
