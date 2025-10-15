import type { Meta, StoryObj } from "@storybook/react";
import ControlBoardStatusCard from "./ControlBoardStatusCard";

const meta: Meta<typeof ControlBoardStatusCard> = {
  title: "ProtoOS/Diagnostic/ControlBoardStatusCard",
  component: ControlBoardStatusCard,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    controlBoardData: {
      control: "object",
      description: "Control board data object",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    controlBoardData: {
      name: "Control Board 1",
      latency: 2.3,
      cpuCapacity: 65.5,
      meta: {},
    },
  },
};

export const WithWarning: Story = {
  args: {
    controlBoardData: {
      name: "Control Board 2",
      latency: 15.8,
      cpuCapacity: 89.2,
      hasWarning: true,
      meta: {},
    },
  },
};

export const LowLatency: Story = {
  args: {
    controlBoardData: {
      name: "Control Board 3",
      latency: 0.8,
      cpuCapacity: 45.2,
      meta: {},
    },
  },
};

export const HighCpuUsage: Story = {
  args: {
    controlBoardData: {
      name: "Control Board 4",
      latency: 8.5,
      cpuCapacity: 95.7,
      hasWarning: true,
      meta: {},
    },
  },
};

export const Critical: Story = {
  args: {
    controlBoardData: {
      name: "Control Board 5",
      latency: 25.4,
      cpuCapacity: 98.9,
      hasWarning: true,
      meta: {},
    },
  },
};
