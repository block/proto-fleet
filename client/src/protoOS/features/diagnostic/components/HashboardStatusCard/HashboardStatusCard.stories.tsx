import type { Meta, StoryObj } from "@storybook/react";
import HashboardStatusCard from "./HashboardStatusCard";

const meta: Meta<typeof HashboardStatusCard> = {
  title: "ProtoOS/Diagnostic/HashboardStatusCard",
  component: HashboardStatusCard,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    hashboardData: {
      control: "object",
      description: "Hashboard data object",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    hashboardData: {
      id: 1,
      name: "Board 1",
      position: 1,
      avgAsicTemp: 65.2,
      maxAsicTemp: 72.1,
    },
    serialNumber: "HB123454",
  },
};

export const WithWarning: Story = {
  args: {
    hashboardData: {
      id: 2,
      name: "Board 2",
      position: 2,
      avgAsicTemp: 78.5,
      maxAsicTemp: 85.3,
      hasWarning: true,
    },
    serialNumber: "HB123455",
  },
};

export const HighPerformance: Story = {
  args: {
    hashboardData: {
      id: 3,
      name: "Board 3",
      position: 3,
      avgAsicTemp: 72.1,
      maxAsicTemp: 78.9,
    },
    serialNumber: "HB123456",
  },
};

export const LowPerformance: Story = {
  args: {
    hashboardData: {
      id: 4,
      name: "Board 4",
      position: 4,
      avgAsicTemp: 58.3,
      maxAsicTemp: 62.7,
      hasWarning: true,
    },
    serialNumber: "HB123457",
  },
};

export const Offline: Story = {
  args: {
    hashboardData: {
      id: 5,
      name: "Board 5",
      position: 5,
      avgAsicTemp: 25.0,
      maxAsicTemp: 25.0,
      hasWarning: true,
    },
    serialNumber: "HB123458",
  },
};
