import { useEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import PsuStatusCard from "./PsuStatusCard";
import useMinerStore from "@/protoOS/store/useMinerStore";

// Configuration for different stories
const storyConfigs: Record<
  number,
  {
    slot: number;
    inputVoltage: number;
    outputVoltage: number;
    inputPower: number;
    outputPower: number;
    temps: number[];
  }
> = {
  1: {
    slot: 1,
    inputVoltage: 220.0,
    outputVoltage: 12.5,
    inputPower: 3200,
    outputPower: 3000,
    temps: [45.0, 48.0, 52.0],
  }, // Default - normal operation
  2: {
    slot: 2,
    inputVoltage: 218.0,
    outputVoltage: 12.3,
    inputPower: 3400,
    outputPower: 3150,
    temps: [62.0, 68.0, 72.0],
  }, // WithWarning - high temps
  3: {
    slot: 3,
    inputVoltage: 222.0,
    outputVoltage: 12.6,
    inputPower: 3800,
    outputPower: 3600,
    temps: [55.0, 58.0, 61.0],
  }, // HighLoad - high power
  4: {
    slot: 4,
    inputVoltage: 215.0,
    outputVoltage: 11.8,
    inputPower: 3900,
    outputPower: 3500,
    temps: [78.0, 82.0, 85.0],
  }, // CriticalState - critical temps and voltage issues
  5: {
    slot: 5,
    inputVoltage: 0,
    outputVoltage: 0,
    inputPower: 0,
    outputPower: 0,
    temps: [25.0, 25.0, 25.0],
  }, // Offline - no power
};

// Store decorator that provides mock data
const StoreDecorator = (Story: any, context: any) => {
  const slot = context.args.slot;

  useEffect(() => {
    const store = useMinerStore.getState();

    // Get config for this slot
    const config = storyConfigs[slot] || {
      slot: 1,
      inputVoltage: 220.0,
      outputVoltage: 12.5,
      inputPower: 3200,
      outputPower: 3000,
      temps: [45.0, 48.0, 52.0],
    };

    // Mock PSU hardware data
    store.hardware.addPsu({
      id: slot,
      slot: config.slot,
      serial: `PSU-${slot.toString().padStart(6, "0")}`,
      manufacturer: "Murata Power Solutions",
      model: "D3K3-W-3000-12-HC4C5",
      hwRevision: "v2.1",
      firmware: {
        appVersion: "2.1.5",
        bootloaderVersion: "1.2.0",
      },
    });

    // Mock PSU telemetry data
    store.telemetry.updatePsuTelemetry(slot, {
      inputVoltage: {
        latest: { value: config.inputVoltage, units: "V" },
        timeSeries: {
          units: "V",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
      outputVoltage: {
        latest: { value: config.outputVoltage, units: "V" },
        timeSeries: {
          units: "V",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
      inputPower: {
        latest: { value: config.inputPower, units: "W" },
        timeSeries: {
          units: "W",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
      outputPower: {
        latest: { value: config.outputPower, units: "W" },
        timeSeries: {
          units: "W",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
      temperatureAmbient: {
        latest: { value: config.temps[0], units: "C" },
        timeSeries: {
          units: "C",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
      temperatureAverage: {
        latest: { value: config.temps[1], units: "C" },
        timeSeries: {
          units: "C",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
      temperatureHotspot: {
        latest: { value: config.temps[2], units: "C" },
        timeSeries: {
          units: "C",
          values: [],
          startTime: Date.now(),
          endTime: Date.now(),
        },
      },
    });
  }, [slot]);

  return <Story />;
};

const meta: Meta<typeof PsuStatusCard> = {
  title: "ProtoOS/Diagnostic/PsuStatusCard",
  component: PsuStatusCard,
  decorators: [StoreDecorator],
  parameters: {
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    slot: {
      control: "number",
      description: "PSU slot number",
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

export const HighLoad: Story = {
  args: {
    slot: 3,
  },
};

export const CriticalState: Story = {
  args: {
    slot: 4,
  },
};

export const Offline: Story = {
  args: {
    slot: 5,
  },
};
