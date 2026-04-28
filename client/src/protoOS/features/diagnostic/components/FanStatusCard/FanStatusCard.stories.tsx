import { useEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import FanStatusCard from "./FanStatusCard";
import useMinerStore from "@/protoOS/store/useMinerStore";

// Configuration for different stories
const storyConfigs: Record<number, { slot: number; rpm: number; pwm: number }> = {
  1: { slot: 1, rpm: 5800, pwm: 65.0 }, // Default - normal operation
  2: { slot: 2, rpm: 3200, pwm: 45.0 }, // WithWarning - low RPM
  3: { slot: 3, rpm: 7200, pwm: 95.0 }, // HighPerformance - high speed
  4: { slot: 4, rpm: 0, pwm: 0 }, // InactiveWithWarning - stopped
};

// Store decorator that provides mock data
const StoreDecorator = (Story: any, context: any) => {
  const slot = context.args.slot;

  useEffect(() => {
    const store = useMinerStore.getState();

    // Get config for this slot
    const config = storyConfigs[slot] || {
      slot: 1,
      rpm: 5800,
      pwm: 65.0,
    };

    // Mock fan hardware data
    store.hardware.addFan({
      slot: slot,
      name: `Fan ${config.slot}`,
    });

    // Mock fan telemetry data
    store.telemetry.updateFanTelemetry(slot, {
      rpm: {
        latest: { value: config.rpm, units: "RPM" },
        timeSeries: {
          units: "RPM",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
      percentage: {
        latest: { value: config.pwm, units: "%" },
        timeSeries: {
          units: "%",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
    });
  }, [slot]);

  return <Story />;
};

const meta: Meta<typeof FanStatusCard> = {
  title: "Proto OS/Diagnostic/FanStatusCard",
  component: FanStatusCard,
  decorators: [StoreDecorator],
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    slot: {
      control: "number",
      description: "Fan slot number",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    slot: 1,
  },
};

export const WithWarning: Story = {
  args: {
    slot: 2,
  },
};

export const HighPerformance: Story = {
  args: {
    slot: 3,
  },
};

export const InactiveWithWarning: Story = {
  args: {
    slot: 4,
  },
};
