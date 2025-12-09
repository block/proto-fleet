import { useMemo } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import { generateUptimeHeadline } from "./utils";
import { type UptimeStatusCount, UptimeStatusCountSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { SegmentedMetricPanel } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel";
import type { SegmentConfig } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";
import type { Duration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

// Uptime segment configuration (same as UptimePanel)
const uptimeSegmentConfig: SegmentConfig = {
  hashing: {
    color: "var(--color-text-primary)",
    label: "Hashing",
    displayInBreakdown: true,
    showButton: false,
    index: 1,
  },
  notHashing: {
    color: "var(--color-core-primary-10)",
    label: "Not hashing",
    displayInBreakdown: true,
    showButton: true,
    buttonVariant: "secondary",
    index: 0,
    onClick: action("navigate-to-miners"),
  },
};

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
  duration: Duration;
  hashingCount: number;
  notHashingCount: number;
}

function MockUptimePanel({ duration, hashingCount, notHashingCount }: MockUptimePanelProps) {
  // Generate multiple data points across the time range to show a proper chart
  const uptimeStatusCounts = useMemo(() => {
    const durationHours = duration === "5d" ? 120 : duration === "48h" ? 48 : parseInt(duration);
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

  return (
    <SegmentedMetricPanel
      title="Uptime"
      headlineGenerator={generateUptimeHeadline}
      chartData={uptimeStatusCounts}
      segmentConfig={uptimeSegmentConfig}
      duration={duration}
    />
  );
}

const meta = {
  title: "Proto Fleet/Dashboard/UptimePanel",
  component: MockUptimePanel,
  tags: ["autodocs"],
  parameters: {
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
      options: ["1h", "12h", "24h", "48h", "5d"],
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
  },
  render: () => {
    const stat = {
      label: "Uptime",
      value: undefined,
      units: "",
    };

    return (
      <div className="flex w-full flex-row overflow-hidden rounded-xl bg-surface-base dark:bg-core-primary-5 phone:flex-col phone:gap-6">
        <ChartWidget stats={stat} className="w-1/2 rounded-none! bg-transparent dark:bg-transparent phone:w-full">
          <SkeletonBar className="h-60 w-full" />
        </ChartWidget>
        <div className="flex w-1/2 flex-col justify-center gap-16 space-y-3 rounded-xl bg-transparent p-10 dark:bg-transparent phone:w-full phone:gap-4 phone:p-6 phone:pt-0">
          <SkeletonBar className="h-20 w-full" />
          <SkeletonBar className="h-20 w-full" />
        </div>
      </div>
    );
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
  render: () => (
    <SegmentedMetricPanel
      title="Uptime"
      headlineGenerator={generateUptimeHeadline}
      chartData={[]}
      segmentConfig={uptimeSegmentConfig}
      duration="24h"
    />
  ),
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
 * 48-hour view showing uptime trends over two days.
 */
export const FortyEightHours: Story = {
  args: {
    duration: "48h",
    hashingCount: 8,
    notHashingCount: 2,
  },
};

/**
 * 5-day view showing uptime patterns over nearly a week.
 */
export const FiveDays: Story = {
  args: {
    duration: "5d",
    hashingCount: 8,
    notHashingCount: 2,
  },
};
