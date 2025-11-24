import type { Meta, StoryObj } from "@storybook/react";
import { TemperaturePanel } from "./TemperaturePanel";

const meta = {
  title: "ProtoFleet/KPIs/TemperaturePanel",
  component: TemperaturePanel,
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "Temperature monitoring panel that displays the distribution of miners across different temperature ranges (Cold, Normal, Hot, Critical) using the SegmentedMetricPanel.",
      },
    },
  },
  decorators: [
    (Story) => (
      <div className="flex h-full w-full items-center justify-center bg-surface-10">
        <div className="w-full p-10">
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof TemperaturePanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// Since TemperaturePanel uses mock data internally for now,
// these stories will all show the same data until the backend is ready
export const Default: Story = {};

// Future stories for when backend is ready:
// - Loading state
// - All normal temperatures
// - High temperature warning
// - Mixed temperature distribution
