import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react";
import { SegmentedMetricPanel } from "./SegmentedMetricPanel";
import type { SegmentConfig } from "./types";
import type { TemperatureStatusCount } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { Triangle } from "@/shared/assets/icons";
import { fleetDurations } from "@/shared/components/DurationSelector";

const meta = {
  title: "Proto Fleet/Dashboard/SegmentedMetricPanel",
  component: SegmentedMetricPanel,
  tags: ["autodocs"],
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "A panel component that combines ChartWidget with a SegmentedBarChart and current status breakdown. Supports granular intervals for short durations and multi-chart display for longer periods.",
      },
    },
  },
  argTypes: {
    title: {
      control: "text",
      description: "The title displayed at the top of the panel",
    },
    headline: {
      control: "text",
      description: "Summary headline shown below the title",
    },
    chartData: {
      control: "object",
      description: "Temperature status count data from the API",
    },
    segmentConfig: {
      control: "object",
      description: "Configuration for each segment (color, label, etc.)",
    },
    duration: {
      control: "select",
      options: fleetDurations,
      description: "Time duration for the chart display",
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
} satisfies Meta<typeof SegmentedMetricPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

// Helper to create timestamp from date
const createTimestamp = (date: Date): Timestamp => {
  const millis = date.getTime();
  const seconds = Math.floor(millis / 1000);
  const nanos = (millis % 1000) * 1000000;
  return {
    seconds: seconds as any, // Protobuf expects bigint but for display we use number
    nanos,
  } as Timestamp;
};

// Generate highly granular mock data (every minute)
const generateGranularData = (
  hours: number,
  basePattern?: {
    cold?: number;
    ok?: number;
    hot?: number;
    critical?: number;
  },
): TemperatureStatusCount[] => {
  const data: TemperatureStatusCount[] = [];
  const now = new Date();
  const minutesTotal = hours * 60;

  // Default pattern if not provided
  const pattern = {
    cold: 2,
    ok: 180,
    hot: 15,
    critical: 3,
    ...basePattern, // Override with provided values
  };

  for (let i = 0; i < minutesTotal; i++) {
    const time = new Date(now.getTime() - (minutesTotal - i - 1) * 60 * 1000);

    // Add some variation to make it realistic
    const variation = Math.sin(i / 10) * 0.1; // ±10% variation

    data.push({
      timestamp: createTimestamp(time),
      coldCount: Math.max(0, Math.floor(pattern.cold + pattern.cold * variation)),
      okCount: Math.max(0, Math.floor(pattern.ok + pattern.ok * variation)),
      hotCount: Math.max(0, Math.floor(pattern.hot + pattern.hot * variation)),
      criticalCount: Math.max(0, Math.floor(pattern.critical + pattern.critical * variation)),
    } as TemperatureStatusCount);
  }

  return data;
};

// Temperature segment configuration
const temperatureSegmentConfig: SegmentConfig = {
  cold: {
    color: "var(--color-intent-info-fill)",
    label: "Cold",
    displayInBreakdown: true,
    index: 2, // Third in order
  },
  ok: {
    color: "var(--color-intent-info-20)",
    label: "Healthy",
    displayInBreakdown: true,
    index: 3, // Fourth in order
    showButton: false,
    percentageLabel: "Within optimal range", // Custom label for normal temperature
  },
  hot: {
    color: "var(--color-intent-warning-fill)",
    label: "Hot",
    displayInBreakdown: true,
    index: 1, // Second in order
  },
  critical: {
    color: "var(--color-intent-critical-fill)",
    label: "Critical",
    displayInBreakdown: true,
    icon: <Triangle />,
    index: 0, // First in order
    buttonVariant: "primary", // Use primary button for critical items
  },
};

// 1 Hour Duration - Shows 5-minute intervals
export const OneHourDuration: Story = {
  args: {
    title: "Temperature",
    headline: "8.5% outside safe range",
    chartData: generateGranularData(1.5), // Generate 1.5 hours of minute-level data
    segmentConfig: temperatureSegmentConfig,
    duration: "1h",
  },
};

// 12 Hour Duration - Shows hourly intervals
export const TwelveHourDuration: Story = {
  args: {
    title: "Temperature",
    headline: "9.0% outside safe range",
    chartData: generateGranularData(13), // Generate 13 hours of minute-level data
    segmentConfig: temperatureSegmentConfig,
    duration: "24h",
  },
};

// 24 Hour Duration - Shows 2-hour intervals
export const TwentyFourHourDuration: Story = {
  args: {
    title: "Temperature",
    headline: "10.0% outside safe range",
    chartData: generateGranularData(25), // Generate 25 hours of minute-level data
    segmentConfig: temperatureSegmentConfig,
    duration: "24h",
  },
};

// 7 Day Duration - Multiple charts with 2 bars per day
export const SevenDayDuration: Story = {
  args: {
    title: "Temperature",
    headline: "9.5% outside safe range",
    chartData: generateGranularData(169), // Generate just over 7 days of minute-level data
    segmentConfig: temperatureSegmentConfig,
    duration: "7d",
  },
};

// 30 Day Duration - Daily bars over a month
export const ThirtyDayDuration: Story = {
  args: {
    title: "Temperature",
    headline: "10.5% outside safe range",
    chartData: generateGranularData(24 * 31), // Generate just over 30 days of minute-level data
    segmentConfig: temperatureSegmentConfig,
    duration: "30d",
  },
};

// With percentage display enabled
export const WithPercentages: Story = {
  args: {
    title: "Temperature",
    headline: "11.0% outside safe range",
    chartData: generateGranularData(24, {
      cold: 5,
      ok: 170,
      hot: 20,
      critical: 5,
    }),
    segmentConfig: temperatureSegmentConfig,
    duration: "24h",
  },
};

// Edge case: Very few miners
export const FewMiners: Story = {
  args: {
    title: "Temperature",
    headline: "25.0% outside safe range",
    chartData: generateGranularData(12, {
      cold: 1,
      ok: 6,
      hot: 2,
      critical: 1,
    }),
    segmentConfig: temperatureSegmentConfig,
    duration: "24h",
  },
};

// Edge case: All miners in one category
export const AllNormal: Story = {
  args: {
    title: "Temperature",
    headline: "0.0% outside safe range",
    chartData: generateGranularData(6, {
      cold: 0,
      ok: 200,
      hot: 0,
      critical: 0,
    }),
    segmentConfig: temperatureSegmentConfig,
    duration: "24h",
  },
};

// Edge case: No data
export const NoData: Story = {
  args: {
    title: "Temperature",
    headline: "No data",
    chartData: [],
    segmentConfig: temperatureSegmentConfig,
    duration: "24h",
  },
};

// Custom segment configuration (different use case)
const uptimeSegmentConfig: SegmentConfig = {
  offline: {
    color: "var(--color-intent-critical-fill)",
    label: "Offline",
    displayInBreakdown: true,
  },
  sleeping: {
    color: "var(--color-intent-warning-fill)",
    label: "Sleeping",
    displayInBreakdown: true,
  },
  broken: {
    color: "var(--color-intent-info-fill)",
    label: "Broken",
    displayInBreakdown: true,
  },
  hashing: {
    color: "var(--color-intent-success-fill)",
    label: "Hashing",
    displayInBreakdown: true,
  },
};

// Alternative use case: Uptime monitoring
export const UptimeMonitoring: Story = {
  args: {
    title: "Uptime",
    headline: "5.0% not hashing",
    chartData: generateGranularData(24, {
      cold: 5, // Using cold for offline
      ok: 190, // Using ok for hashing
      hot: 3, // Using hot for sleeping
      critical: 2, // Using critical for broken
    }),
    segmentConfig: uptimeSegmentConfig,
    duration: "24h",
  },
};
