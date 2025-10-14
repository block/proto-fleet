import type { Meta, StoryObj } from "@storybook/react";
import FanStatusCard from "./FanStatusCard";

const meta: Meta<typeof FanStatusCard> = {
  title: "ProtoOS/Diagnostic/FanStatusCard",
  component: FanStatusCard,
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    fanData: {
      control: "object",
      description: "Fan data object",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    fanData: {
      id: 1,
      position: 1,
      name: "Fan 1",
      rpm: 7500,
      pwm: 85.0,
      meta: {},
    },
  },
};

export const WithWarning: Story = {
  args: {
    fanData: {
      id: 1,
      position: 2,
      name: "Fan 2",
      rpm: 7200,
      pwm: 82.0,
      hasWarning: true,
      meta: {},
    },
  },
};

export const HighPerformance: Story = {
  args: {
    fanData: {
      id: 1,
      position: 3,
      name: "Fan 3",
      rpm: 8304,
      pwm: 90.4,
      hasWarning: true,
      meta: {},
    },
  },
};

export const InactiveWithWarning: Story = {
  args: {
    fanData: {
      id: 1,
      position: 4,
      name: "Fan 4",
      rpm: 0,
      pwm: 0,
      hasWarning: true,
      meta: {},
    },
  },
};
