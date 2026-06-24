import type { Meta, StoryObj } from "@storybook/react";

import FleetInfraPage from "./FleetInfraPage";
import { mockInfraDevices } from "@/protoFleet/features/infrastructure/mockInfraDevices";

const meta = {
  title: "Proto Fleet/Fleet/Infrastructure",
  component: FleetInfraPage,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base">
        <div className="sticky left-0 z-10 flex flex-col gap-4 bg-surface-base px-6 pt-6 laptop:px-10">
          <h1 className="text-heading-300 text-text-primary">Fleet</h1>
        </div>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof FleetInfraPage>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    devices: mockInfraDevices,
    canManage: true,
  },
};

export const Empty: Story = {
  args: {
    devices: [],
    canManage: true,
  },
};
