import type { Meta, StoryObj } from "@storybook/react";
import PsuStatusCard from "./PsuStatusCard";

const meta: Meta<typeof PsuStatusCard> = {
  title: "ProtoOS/Diagnostic/PsuStatusCard",
  component: PsuStatusCard,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    psuData: {
      control: "object",
      description: "PSU data object",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    psuData: {
      id: 1,
      name: "PSU 1",
      position: 1,
      inputVoltage: 110.0,
      outputVoltage: 12.0,
      inputPower: 120.0,
      outputPower: 102.0,
      avgTemp: 45.2,
      maxTemp: 52.1,
      meta: {},
    },
  },
};

export const WithWarning: Story = {
  args: {
    psuData: {
      id: 2,
      name: "PSU 2",
      position: 2,
      inputVoltage: 108.5,
      outputVoltage: 11.8,
      inputPower: 135.0,
      outputPower: 120.4,
      avgTemp: 58.7,
      maxTemp: 65.3,
      hasWarning: true,
      meta: {},
    },
  },
};

export const HighLoad: Story = {
  args: {
    psuData: {
      id: 3,
      name: "PSU 3",
      position: 3,
      inputVoltage: 112.0,
      outputVoltage: 12.1,
      inputPower: 210.0,
      outputPower: 191.2,
      avgTemp: 62.1,
      maxTemp: 68.5,
      meta: {},
    },
  },
};

export const CriticalState: Story = {
  args: {
    psuData: {
      id: 4,
      name: "PSU 4",
      position: 4,
      inputVoltage: 105.0,
      outputVoltage: 11.2,
      inputPower: 240.0,
      outputPower: 211.7,
      avgTemp: 75.3,
      maxTemp: 82.1,
      hasWarning: true,
      meta: {},
    },
  },
};

export const Offline: Story = {
  args: {
    psuData: {
      id: 5,
      name: "PSU 5",
      position: 5,
      inputVoltage: 0.0,
      outputVoltage: 0.0,
      inputPower: 0.0,
      outputPower: 0.0,
      avgTemp: 25.0,
      maxTemp: 25.0,
      hasWarning: true,
      meta: {},
    },
  },
};
