import { useMemo } from "react";
import { MemoryRouter } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { UptimePanel } from "./UptimePanel";
import { type UptimeStatusCount, UptimeStatusCountSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { durationToHours } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/utils";
import { type FleetDuration, fleetDurations } from "@/shared/components/DurationSelector";

// Helper to create mock uptime status counts
const createMockUptimeStatusCount = (
  timestampSeconds: number,
  hashingCount: number,
  notHashingCount: number,
): UptimeStatusCount => {
  return create(UptimeStatusCountSchema, {
    timestamp: {
      seconds: BigInt(timestampSeconds),
      nanos: 0,
    },
    hashingCount,
    notHashingCount,
  });
};

// Mock UptimePanel component for Storybook
interface MockUptimePanelProps {
  duration: FleetDuration;
  hashingCount: number;
  notHashingCount: number;
  isLoading?: boolean; // Used to set uptimeStatusCounts to undefined
}

function MockUptimePanel({ duration, hashingCount, notHashingCount, isLoading = false }: MockUptimePanelProps) {
  // Generate multiple data points across the time range to show a proper chart
  const uptimeStatusCounts = useMemo(() => {
    const durationHours = durationToHours(duration);
    const intervalCount = 12; // Match the number of bars in the chart
    const intervalHours = durationHours / intervalCount;

    const counts: UptimeStatusCount[] = [];
    // eslint-disable-next-line react-hooks/purity
    const now = Math.floor(Date.now() / 1000);
    const totalMiners = hashingCount + notHashingCount;

    // Create data points for each interval
    // Historical bars show 100% hashing, most recent bar shows actual state
    for (let i = 0; i < intervalCount; i++) {
      const hoursAgo = durationHours - i * intervalHours;
      const timestampSeconds = now - Math.floor(hoursAgo * 3600);

      // For the most recent bar (i === intervalCount - 1), use the exact props
      // For historical bars, show all miners hashing
      const isLatestBar = i === intervalCount - 1;
      const barHashingCount = isLatestBar ? hashingCount : totalMiners;
      const barNotHashingCount = isLatestBar ? notHashingCount : 0;

      counts.push(createMockUptimeStatusCount(timestampSeconds, barHashingCount, barNotHashingCount));
    }

    return counts;
  }, [duration, hashingCount, notHashingCount]);

  return <UptimePanel duration={duration} uptimeStatusCounts={isLoading ? undefined : uptimeStatusCounts} />;
}

const meta = {
  title: "Proto Fleet/Dashboard/UptimePanel",
  component: MockUptimePanel,
  tags: ["autodocs"],
  parameters: {
    withRouter: false,
    layout: "padded",
    docs: {
      description: {
        component:
          "Uptime monitoring panel that displays the distribution of miners between hashing and not hashing states using the SegmentedMetricPanel. Shows real-time streaming updates of miner uptime status.",
      },
    },
  },
  decorators: [
    (Story) => (
      <MemoryRouter>
        <div className="flex h-full w-full items-center justify-center bg-surface-10">
          <div className="w-full p-10">
            <Story />
          </div>
        </div>
      </MemoryRouter>
    ),
  ],
  argTypes: {
    duration: {
      control: "select",
      options: fleetDurations,
      description: "Time range for the uptime data",
    },
    hashingCount: {
      control: { type: "number", min: 0, max: 100 },
      description: "Number of miners currently hashing",
    },
    notHashingCount: {
      control: { type: "number", min: 0, max: 100 },
      description: "Number of miners not currently hashing",
    },
  },
} satisfies Meta<typeof MockUptimePanel>;

export default meta;
type Story = StoryObj<typeof meta>;

/**
 * Default 24-hour view with typical uptime data.
 * Shows 8 miners hashing and 2 not hashing (20% downtime).
 */
export const Default: Story = {
  args: {
    duration: "24h",
    hashingCount: 8,
    notHashingCount: 2,
  },
};

/**
 * Loading state showing skeleton loaders while data is being fetched.
 */
export const Loading: Story = {
  args: {
    duration: "24h",
    hashingCount: 0,
    notHashingCount: 0,
    isLoading: true,
  },
};

/**
 * No data state - shown when there is no telemetry data available.
 * Displays "No data" message.
 */
export const NoData: Story = {
  args: {
    duration: "24h",
    hashingCount: 0,
    notHashingCount: 0,
  },
  render: (args) => {
    return <MockUptimePanel {...args} />;
  },
};

/**
 * No miners state - shown when there are 0 total miners.
 * Displays "No miners" message.
 */
export const NoMiners: Story = {
  args: {
    duration: "24h",
    hashingCount: 0,
    notHashingCount: 0,
  },
};

/**
 * All miners not hashing - critical state with 100% downtime.
 * Shows "100% not hashing" headline with action button.
 */
export const AllNotHashing: Story = {
  args: {
    duration: "24h",
    hashingCount: 0,
    notHashingCount: 10,
  },
};

/**
 * All miners are hashing - ideal state with 100% uptime.
 * Shows "All miners hashing" headline with no action button.
 */
export const AllHashing: Story = {
  args: {
    duration: "24h",
    hashingCount: 10,
    notHashingCount: 0,
  },
};

/**
 * One miner not hashing - shows singular "1 miner" text.
 * Displays action button with count and percentage (10%).
 */
export const OneMinerDown: Story = {
  args: {
    duration: "24h",
    hashingCount: 9,
    notHashingCount: 1,
  },
};

/**
 * Multiple miners not hashing - shows plural "miners" text.
 * Displays "2 miners not hashing (20%)" with action button.
 */
export const MultipleMinersDown: Story = {
  args: {
    duration: "24h",
    hashingCount: 8,
    notHashingCount: 2,
  },
};

/**
 * Significant downtime - half the fleet not hashing.
 * Shows "5 miners not hashing (50%)" in critical state.
 */
export const SignificantDowntime: Story = {
  args: {
    duration: "24h",
    hashingCount: 5,
    notHashingCount: 5,
  },
};

/**
 * Large fleet with some miners down.
 * Demonstrates scaling with 50 miners total.
 */
export const LargeFleet: Story = {
  args: {
    duration: "24h",
    hashingCount: 45,
    notHashingCount: 5,
  },
};

/**
 * 7-day view showing uptime trends over a full week.
 */
export const OneWeek: Story = {
  args: {
    duration: "7d",
    hashingCount: 8,
    notHashingCount: 2,
  },
};

/**
 * 30-day view showing uptime patterns over a month.
 */
export const ThirtyDays: Story = {
  args: {
    duration: "30d",
    hashingCount: 8,
    notHashingCount: 2,
  },
};
