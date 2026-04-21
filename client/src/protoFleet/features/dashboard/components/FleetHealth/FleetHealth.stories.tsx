import { BrowserRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react";
import FleetHealth from "./FleetHealth";

const meta: Meta<typeof FleetHealth> = {
  title: "Proto Fleet/Dashboard/FleetHealth",
  component: FleetHealth,
  parameters: {
    withRouter: false,
    layout: "centered",
    docs: {
      description: {
        component: "Displays fleet health statistics with a composition bar visualization",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    fleetSize: {
      control: { type: "number", min: 0, max: 1000, step: 1 },
      description: "Total number of miners in the fleet",
    },
    healthyMiners: {
      control: { type: "number", min: 0, max: 1000, step: 1 },
      description: "Number of healthy/active miners",
    },
    needsAttentionMiners: {
      control: { type: "number", min: 0, max: 1000, step: 1 },
      description: "Number of miners needing attention (ERROR or AUTHENTICATION_NEEDED)",
    },
    offlineMiners: {
      control: { type: "number", min: 0, max: 1000, step: 1 },
      description: "Number of offline miners",
    },
    sleepingMiners: {
      control: { type: "number", min: 0, max: 1000, step: 1 },
      description: "Number of sleeping/inactive miners",
    },
  },
  decorators: [
    (Story) => (
      <BrowserRouter>
        <div className="w-[800px] p-4">
          <Story />
        </div>
      </BrowserRouter>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof FleetHealth>;

export const Default: Story = {
  args: {
    fleetSize: 200,
    healthyMiners: 178,
    needsAttentionMiners: 15,
    offlineMiners: 5,
    sleepingMiners: 2,
  },
};

export const AllHealthy: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 100,
    needsAttentionMiners: 0,
    offlineMiners: 0,
    sleepingMiners: 0,
  },
};

export const MostlyHealthy: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 85,
    needsAttentionMiners: 5,
    offlineMiners: 5,
    sleepingMiners: 5,
  },
};

export const Warning: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 70,
    needsAttentionMiners: 15,
    offlineMiners: 10,
    sleepingMiners: 5,
  },
};

export const Critical: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 30,
    needsAttentionMiners: 40,
    offlineMiners: 20,
    sleepingMiners: 10,
  },
};

export const SmallFleet: Story = {
  args: {
    fleetSize: 10,
    healthyMiners: 7,
    needsAttentionMiners: 1,
    offlineMiners: 1,
    sleepingMiners: 1,
  },
};

export const LargeFleet: Story = {
  args: {
    fleetSize: 1000,
    healthyMiners: 850,
    needsAttentionMiners: 80,
    offlineMiners: 50,
    sleepingMiners: 20,
  },
};

export const Loading: Story = {
  args: {
    // All props undefined to show loading state
  },
};

export const PartialLoading: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 70,
    // needsAttentionMiners, offlineMiners, and sleepingMiners undefined
  },
};
