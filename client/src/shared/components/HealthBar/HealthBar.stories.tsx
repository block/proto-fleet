import type { Meta, StoryObj } from "@storybook/react";
import HealthBar from "./HealthBar";
import type { HealthBarProps } from "./HealthBar";

const meta: Meta<HealthBarProps> = {
  title: "Shared/HealthBar",
  component: HealthBar,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "A segmented health bar showing device status distribution. Healthy segment renders as a thin 2px line; issue states render at full height.",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    healthy: { control: { type: "number", min: 0, max: 500 } },
    needsAttention: { control: { type: "number", min: 0, max: 500 } },
    offline: { control: { type: "number", min: 0, max: 500 } },
    sleeping: { control: { type: "number", min: 0, max: 500 } },
    empty: { control: { type: "number", min: 0, max: 500 } },
  },
  decorators: [
    (Story) => (
      <div className="w-96 p-4">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<HealthBarProps>;

export const AllHealthy: Story = {
  args: { healthy: 100, needsAttention: 0, offline: 0, sleeping: 0 },
};

export const Mixed: Story = {
  args: { healthy: 60, needsAttention: 15, offline: 8, sleeping: 12 },
};

export const AllIssues: Story = {
  args: { healthy: 0, needsAttention: 30, offline: 10, sleeping: 5 },
};

export const MostlyHealthy: Story = {
  args: { healthy: 95, needsAttention: 3, offline: 1, sleeping: 1 },
};

export const Empty: Story = {
  args: { healthy: 0, needsAttention: 0, offline: 0, sleeping: 0 },
};

export const WithEmptySlots: Story = {
  args: { healthy: 40, needsAttention: 5, offline: 2, sleeping: 3, empty: 10 },
};
