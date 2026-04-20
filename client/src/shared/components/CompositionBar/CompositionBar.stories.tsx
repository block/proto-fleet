import type { Meta, StoryObj } from "@storybook/react";
import CompositionBar from "./CompositionBar";
import type { CompositionBarProps } from "./types";

const meta: Meta<CompositionBarProps> = {
  title: "Shared/CompositionBar",
  component: CompositionBar,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "A horizontal bar chart component for visualizing data composition with colored segments representing different statuses.",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    segments: {
      description: "Array of segments with name, status, and count",
      control: "object",
    },
    height: {
      description: "Height of the bar in pixels",
      control: { type: "number", min: 2, max: 20, step: 1 },
      defaultValue: 8,
    },
    gap: {
      description: "Gap between segments (Tailwind gap value)",
      control: { type: "number", min: 0, max: 12, step: 1 },
      defaultValue: 2,
    },
    className: {
      description: "Optional custom CSS classes",
      control: "text",
    },
    colorMap: {
      description: "Optional custom color mappings for status values",
      control: "object",
    },
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
type Story = StoryObj<CompositionBarProps>;

/**
 * Default composition bar with mixed statuses
 */
export const Default: Story = {
  args: {
    segments: [
      { name: "Healthy", status: "OK", count: 45 },
      { name: "Warning", status: "WARNING", count: 10 },
      { name: "Critical", status: "CRITICAL", count: 5 },
      { name: "Offline", status: "NA", count: 2 },
    ],
  },
};

/**
 * Custom color mapping example (Fleet Health style)
 */
export const CustomColors: Story = {
  args: {
    segments: [
      { name: "Healthy", status: "OK", count: 85 },
      { name: "Unhealthy", status: "CRITICAL", count: 10 },
      { name: "Offline", status: "NA", count: 5 },
    ],
    height: 12,
    gap: 2,
    colorMap: {
      OK: "bg-core-primary-fill",
      NA: "bg-core-primary-20",
    },
  },
};

/**
 * Loading state - shows skeleton bar when all counts are undefined
 */
export const Loading: Story = {
  args: {
    segments: [
      { name: "Healthy", status: "OK", count: undefined },
      { name: "Unhealthy", status: "CRITICAL", count: undefined },
      { name: "Offline", status: "NA", count: undefined },
    ],
    height: 12,
  },
};
