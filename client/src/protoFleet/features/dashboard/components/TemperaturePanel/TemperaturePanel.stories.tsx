import { useMemo } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { TemperaturePanel } from "./TemperaturePanel";
import {
  type TemperatureStatusCount,
  TemperatureStatusCountSchema,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { durationToHours } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/utils";
import { type FleetDuration, fleetDurations } from "@/shared/components/DurationSelector";

// Helper to create mock temperature status counts
const createMockTemperatureStatusCount = (
  timestampSeconds: number,
  coldCount: number,
  okCount: number,
  hotCount: number,
  criticalCount: number,
): TemperatureStatusCount => {
  return create(TemperatureStatusCountSchema, {
    timestamp: {
      seconds: BigInt(timestampSeconds),
      nanos: 0,
    },
    coldCount,
    okCount,
    hotCount,
    criticalCount,
  });
};

// Mock TemperaturePanel component for Storybook
interface MockTemperaturePanelProps {
  duration: FleetDuration;
  coldCount: number;
  okCount: number;
  hotCount: number;
  criticalCount: number;
  isLoading?: boolean; // Used to set temperatureStatusCounts to undefined
}

function MockTemperaturePanel({
  duration,
  coldCount,
  okCount,
  hotCount,
  criticalCount,
  isLoading = false,
}: MockTemperaturePanelProps) {
  // Generate multiple data points across the time range
  const temperatureStatusCounts = useMemo(() => {
    const durationHours = durationToHours(duration);
    const intervalCount = 12;
    const intervalHours = durationHours / intervalCount;

    const counts: TemperatureStatusCount[] = [];
    // eslint-disable-next-line react-hooks/purity
    const now = Math.floor(Date.now() / 1000);

    // Create data points for each interval
    for (let i = 0; i < intervalCount; i++) {
      const hoursAgo = durationHours - i * intervalHours;
      const timestampSeconds = now - Math.floor(hoursAgo * 3600);

      // For the most recent bar, use the exact props
      // For historical bars, show all OK temps
      const isLatestBar = i === intervalCount - 1;
      const totalCount = coldCount + okCount + hotCount + criticalCount;
      const barColdCount = isLatestBar ? coldCount : 0;
      const barOkCount = isLatestBar ? okCount : totalCount;
      const barHotCount = isLatestBar ? hotCount : 0;
      const barCriticalCount = isLatestBar ? criticalCount : 0;

      counts.push(
        createMockTemperatureStatusCount(timestampSeconds, barColdCount, barOkCount, barHotCount, barCriticalCount),
      );
    }

    return counts;
  }, [duration, coldCount, okCount, hotCount, criticalCount]);

  return (
    <TemperaturePanel duration={duration} temperatureStatusCounts={isLoading ? undefined : temperatureStatusCounts} />
  );
}

const meta = {
  title: "Proto Fleet/Dashboard/TemperaturePanel",
  component: MockTemperaturePanel,
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
  argTypes: {
    duration: {
      control: "select",
      options: fleetDurations,
      description: "Time range for the temperature data",
    },
    coldCount: {
      control: { type: "number", min: 0, max: 100 },
      description: "Number of miners running cold",
    },
    okCount: {
      control: { type: "number", min: 0, max: 100 },
      description: "Number of miners at healthy temperature",
    },
    hotCount: {
      control: { type: "number", min: 0, max: 100 },
      description: "Number of miners running hot",
    },
    criticalCount: {
      control: { type: "number", min: 0, max: 100 },
      description: "Number of miners at critical temperature",
    },
  },
} satisfies Meta<typeof MockTemperaturePanel>;

export default meta;
type Story = StoryObj<typeof meta>;

/**
 * Default 24-hour view with typical temperature distribution.
 * Shows mostly healthy temps with a few hot miners.
 */
export const Default: Story = {
  args: {
    duration: "24h",
    coldCount: 0,
    okCount: 8,
    hotCount: 2,
    criticalCount: 0,
  },
};

/**
 * Loading state showing skeleton loaders.
 */
export const Loading: Story = {
  args: {
    duration: "24h",
    coldCount: 0,
    okCount: 0,
    hotCount: 0,
    criticalCount: 0,
    isLoading: true,
  },
};

/**
 * All miners at healthy temperature - ideal state.
 */
export const AllHealthy: Story = {
  args: {
    duration: "24h",
    coldCount: 0,
    okCount: 10,
    hotCount: 0,
    criticalCount: 0,
  },
};

/**
 * Some miners running hot - warning state.
 */
export const HighTemperatureWarning: Story = {
  args: {
    duration: "24h",
    coldCount: 0,
    okCount: 7,
    hotCount: 3,
    criticalCount: 0,
  },
};

/**
 * Critical temperature alert - some miners overheating.
 */
export const CriticalTemperature: Story = {
  args: {
    duration: "24h",
    coldCount: 0,
    okCount: 6,
    hotCount: 2,
    criticalCount: 2,
  },
};

/**
 * Mixed temperature distribution across all ranges.
 */
export const MixedDistribution: Story = {
  args: {
    duration: "24h",
    coldCount: 1,
    okCount: 6,
    hotCount: 2,
    criticalCount: 1,
  },
};
