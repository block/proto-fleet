import { BrowserRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react";
import FleetHealth from "./FleetHealth";

const meta: Meta<typeof FleetHealth> = {
  title: "Proto Fleet/Dashboard/FleetHealth",
  component: FleetHealth,
  parameters: {
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
    unhealthyMiners: {
      control: { type: "number", min: 0, max: 1000, step: 1 },
      description: "Number of unhealthy/inactive miners",
    },
    offlineMiners: {
      control: { type: "number", min: 0, max: 1000, step: 1 },
      description: "Number of offline miners",
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
    unhealthyMiners: 20,
    offlineMiners: 2,
  },
};

export const AllHealthy: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 100,
    unhealthyMiners: 0,
    offlineMiners: 0,
  },
};

export const MostlyHealthy: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 85,
    unhealthyMiners: 10,
    offlineMiners: 5,
  },
};

export const Warning: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 70,
    unhealthyMiners: 20,
    offlineMiners: 10,
  },
};

export const Critical: Story = {
  args: {
    fleetSize: 100,
    healthyMiners: 30,
    unhealthyMiners: 50,
    offlineMiners: 20,
  },
};

export const SmallFleet: Story = {
  args: {
    fleetSize: 10,
    healthyMiners: 7,
    unhealthyMiners: 2,
    offlineMiners: 1,
  },
};

export const LargeFleet: Story = {
  args: {
    fleetSize: 1000,
    healthyMiners: 850,
    unhealthyMiners: 120,
    offlineMiners: 30,
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
    // unhealthyMiners and offlineMiners undefined
  },
};
