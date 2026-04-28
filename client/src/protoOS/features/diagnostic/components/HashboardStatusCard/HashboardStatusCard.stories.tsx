import { useEffect } from "react";
import { MemoryRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react";
import HashboardStatusCard from "./HashboardStatusCard";
import useMinerStore from "@/protoOS/store/useMinerStore";

// Configuration for different stories
const storyConfigs: Record<string, { slot: number; avg: number; max: number }> = {
  HB123454: { slot: 1, avg: 65.2, max: 72.1 }, // Default
  HB123455: { slot: 2, avg: 78.5, max: 85.3 }, // WithWarning - high temps
  HB123456: { slot: 3, avg: 72.1, max: 78.9 }, // HighPerformance
  HB123457: { slot: 4, avg: 58.3, max: 62.7 }, // LowPerformance - low temps
  HB123458: { slot: 5, avg: 25.0, max: 25.0 }, // Offline - room temp
};

// Store decorator that provides mock data
const StoreDecorator = (Story: any, context: any) => {
  const serialNumber = context.args.serialNumber;

  useEffect(() => {
    const store = useMinerStore.getState();

    // Get config for this serial number
    const config = storyConfigs[serialNumber] || {
      slot: 1,
      avg: 65.0,
      max: 70.0,
    };

    // Generate mock ASIC IDs for a typical hashboard (e.g., 126 ASICs)
    const asicIds: string[] = [];
    const asics: any[] = [];
    const asicTelemetry = new Map<string, any>();

    // Temperature variation logic based on story config
    const baseTemp = config.avg;
    const tempRange = config.max - config.avg;

    for (let i = 0; i < 126; i++) {
      const asicId = `${serialNumber}-asic-${i}`;
      const row = Math.floor(i / 21); // 21 ASICs per row
      const column = i % 21;

      asicIds.push(asicId);

      // Create ASIC with proper structure
      asics.push({
        id: asicId,
        hashboardSerial: serialNumber,
        row,
        column,
      });

      // Generate temperature based on position - typically hotter in the middle
      let tempOffset = 0;

      if (config.avg > 30) {
        // Only add variation for active hashboards (not offline)
        // Middle rows and columns tend to be hotter
        const rowHeatFactor = 1 - Math.abs(row - 2.5) / 3; // Peak at middle row (2.5)
        const colHeatFactor = 1 - Math.abs(column - 10) / 10; // Peak at middle column (10)
        const positionHeatFactor = (rowHeatFactor + colHeatFactor) / 2;

        // Add some randomness
        const randomFactor = Math.random() * 0.3 - 0.15; // -15% to +15%

        // Calculate temperature offset
        tempOffset = tempRange * positionHeatFactor + tempRange * randomFactor;
      }

      const asicTemp = baseTemp + tempOffset;

      // Store telemetry data
      asicTelemetry.set(asicId, {
        temperature: {
          unit: "C",
          latest: { value: asicTemp, timestamp: Date.now() },
          timeSeries: [],
        },
      });
    }

    // Mock hashboard hardware data - just add this hashboard
    store.hardware.addHashboard({
      serial: serialNumber,
      slot: config.slot,
      bay: 0,
      asicIds,
    });

    // Batch add all ASICs
    store.hardware.batchAddAsics(asics);

    // Add ASIC telemetry data to the store using setState to trigger Immer properly
    useMinerStore.setState((state) => {
      asicTelemetry.forEach((telemetry, asicId) => {
        const existingAsicTelemetry = state.telemetry.asics.get(asicId) || {};
        state.telemetry.asics.set(asicId, {
          ...existingAsicTelemetry,
          ...telemetry,
        });
      });
    });

    // Mock hashboard telemetry data with temperature
    store.telemetry.updateHashboardTemperatures(
      serialNumber,
      undefined, // inletTemp
      undefined, // outletTemp
      { value: config.avg, units: "C" }, // avgAsicTemp
      { value: config.max, units: "C" }, // maxAsicTemp
    );
  }, [serialNumber]);

  return <Story />;
};

const meta: Meta<typeof HashboardStatusCard> = {
  title: "Proto OS/Diagnostic/HashboardStatusCard",
  component: HashboardStatusCard,
  decorators: [
    (Story) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
    StoreDecorator,
  ],
  parameters: {
    withRouter: false,
    layout: "padded",
  },
  tags: ["autodocs"],
  argTypes: {
    serialNumber: {
      control: "text",
      description: "Hashboard serial number",
    },
  },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    serialNumber: "HB123454",
  },
};

export const WithWarning: Story = {
  args: {
    serialNumber: "HB123455",
  },
};

export const HighPerformance: Story = {
  args: {
    serialNumber: "HB123456",
  },
};

export const LowPerformance: Story = {
  args: {
    serialNumber: "HB123457",
  },
};

export const Offline: Story = {
  args: {
    serialNumber: "HB123458",
  },
};
