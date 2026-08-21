import type { Meta, StoryObj } from "@storybook/react";

import HistoryTab from "./HistoryTab";

const meta = {
  title: "Proto Fleet/Maintenance/Repair history",
  component: HistoryTab,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Review completed repairs, their resolution and technician, filtering controls, CSV export placement, and completed-ticket detail navigation.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-6 laptop:p-10">
        <Story />
      </div>
    ),
  ],
  tags: ["autodocs"],
} satisfies Meta<typeof HistoryTab>;

export default meta;
type Story = StoryObj<typeof meta>;

export const CompletedRepairs: Story = {};
