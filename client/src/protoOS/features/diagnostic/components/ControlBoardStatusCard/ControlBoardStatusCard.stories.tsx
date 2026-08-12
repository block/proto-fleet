import type { Meta, StoryObj } from "@storybook/react";
import ControlBoardStatusCard from "./ControlBoardStatusCard";

const meta: Meta<typeof ControlBoardStatusCard> = {
  title: "Proto OS/Diagnostic/ControlBoardStatusCard",
  component: ControlBoardStatusCard,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    title: {
      control: "text",
      description: "Card title (container modules pass 'Controller 1' / 'Controller 2').",
    },
    latency: {
      control: "number",
      description: "Latency in ms. Rendered only for container controllers; omitted for rigs.",
    },
    cpuCapacity: {
      control: "number",
      description: "CPU load percent. Falls back to the store value when omitted.",
    },
    hasWarning: {
      control: "boolean",
      description: "Forces the warning (Alert) icon, in addition to any store-reported errors.",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    title: "Controller 1",
    latency: 2.3,
    cpuCapacity: 65.5,
  },
};

export const WithWarning: Story = {
  args: {
    title: "Controller 2",
    latency: 15.8,
    cpuCapacity: 89.2,
    hasWarning: true,
  },
};

export const LowLatency: Story = {
  args: {
    title: "Controller 1",
    latency: 0.8,
    cpuCapacity: 45.2,
  },
};

export const HighCpuUsage: Story = {
  args: {
    title: "Controller 2",
    latency: 8.5,
    cpuCapacity: 95.7,
    hasWarning: true,
  },
};

export const Critical: Story = {
  args: {
    title: "Controller 2",
    latency: 25.4,
    cpuCapacity: 98.9,
    hasWarning: true,
  },
};
