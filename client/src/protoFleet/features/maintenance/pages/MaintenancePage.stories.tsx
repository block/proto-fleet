import type { Meta, StoryObj } from "@storybook/react";

import MaintenancePage from "./MaintenancePage";

const meta = {
  title: "Proto Fleet/Maintenance/Workspace",
  component: MaintenancePage,
  parameters: {
    layout: "fullscreen",
    docs: {
      description: {
        component:
          "Review the mock-backed maintenance queue in its page layout. The filters, list/board switcher, ticket rows, and create-ticket action are interactive.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base">
        <Story />
      </div>
    ),
  ],
  tags: ["autodocs"],
} satisfies Meta<typeof MaintenancePage>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Queue: Story = {};
