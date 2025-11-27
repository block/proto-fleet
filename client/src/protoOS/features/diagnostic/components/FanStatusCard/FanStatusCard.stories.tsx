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
  const fanId = context.args.fanId;

  useEffect(() => {
    const store = useMinerStore.getState();

    // Get config for this fan ID
    const config = storyConfigs[fanId] || {
      slot: 1,
      rpm: 5800,
      pwm: 65.0,
    };

    // Mock fan hardware data
    store.hardware.addFan({
      id: fanId,
      slot: config.slot,
      name: `Fan ${config.slot}`,
    });

    // Mock fan telemetry data
    store.telemetry.updateFanTelemetry(fanId, {
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
  }, [fanId]);

  return <Story />;
};

const meta: Meta<typeof FanStatusCard> = {
  title: "ProtoOS/Diagnostic/FanStatusCard",
  component: FanStatusCard,
  decorators: [StoreDecorator],
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    fanId: {
      control: "number",
      description: "Fan ID",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    fanId: 1,
  },
};

export const WithWarning: Story = {
  args: {
    fanId: 2,
  },
};

export const HighPerformance: Story = {
  args: {
    fanId: 3,
  },
};

export const InactiveWithWarning: Story = {
  args: {
    fanId: 4,
  },
};
