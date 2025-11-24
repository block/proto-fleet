import React from "react";
import SegmentedBarChart, {
  type SegmentedBarChartData,
  type SegmentedBarChartProps,
} from ".";

// Generate mock data for stories
const generateMockData = (
  points: number = 12,
  startTime: number = Date.now() - 12 * 60 * 60 * 1000,
): SegmentedBarChartData[] => {
  return Array.from({ length: points }, (_, index) => {
    const timestamp = startTime + index * 60 * 60 * 1000; // Hourly data

    // Generate random values
    const active = Math.floor(Math.random() * 40) + 30;
    const inactive = Math.floor(Math.random() * 30) + 20;
    const error = Math.floor(Math.random() * 20) + 10;

    return {
      datetime: timestamp,
      active,
      inactive,
      error,
    };
  });
};

// Generate mock data with varying totals to demonstrate percentage display
const generatePercentageData = (
  points: number = 12,
): SegmentedBarChartData[] => {
  const startTime = Date.now() - 12 * 60 * 60 * 1000;

  return Array.from({ length: points }, (_, index) => {
    const timestamp = startTime + index * 60 * 60 * 1000;

    // Generate values with varying totals (not percentages)
    // Total will vary between 150-350
    const baseTotal = 150 + Math.floor(Math.random() * 200);

    // Generate random proportions
    const running = Math.floor(baseTotal * (0.1 + Math.random() * 0.2)); // 10-30% of total
    const idle = Math.floor(baseTotal * (0.4 + Math.random() * 0.2)); // 40-60% of total
    const maintenance = Math.floor(baseTotal * (0.05 + Math.random() * 0.1)); // 5-15% of total
    const offline = baseTotal - running - idle - maintenance; // Remainder

    return {
      datetime: timestamp,
      running,
      idle,
      maintenance,
      offline,
    };
  });
};

// Generate mock data for online/offline status (as device counts)
const generateOnlineOfflineData = (
  points: number = 12,
): SegmentedBarChartData[] => {
  const startTime = Date.now() - 12 * 60 * 60 * 1000;

  return Array.from({ length: points }, (_, index) => {
    const timestamp = startTime + index * 60 * 60 * 1000;

    // Generate realistic device counts
    // Total devices varies between 80-120
    const totalDevices = 80 + Math.floor(Math.random() * 40);
    const offlinePercent = Math.random() * 0.2; // 0-20% offline
    const offline = Math.floor(totalDevices * offlinePercent);
    const online = totalDevices - offline;

    return {
      datetime: timestamp,
      online,
      offline,
    };
  });
};

type StoryType = SegmentedBarChartProps;

// Container wrapper for consistent sizing and centering
const StoryContainer = ({ children }: { children: React.ReactNode }) => (
  <div className="flex h-screen w-full items-center justify-center bg-surface-5">
    <div className="w-[600px] rounded-lg bg-surface-base p-4">{children}</div>
  </div>
);

export const Default = (props: StoryType) => {
  return (
    <StoryContainer>
      <SegmentedBarChart {...props} />
    </StoryContainer>
  );
};

Default.args = {
  chartData: generateMockData(12),
  segmentKeys: ["active", "inactive", "error"],
  showTooltip: true,
  segmentsLabel: "Hashrate",
  units: " TH/S",
  yAxisPadding: 0.15, // 15% padding above max value
  xAxisTickInterval: 2,
};

export const PercentageDisplay = (props: StoryType) => {
  return (
    <StoryContainer>
      <SegmentedBarChart {...props} />
    </StoryContainer>
  );
};

PercentageDisplay.args = {
  chartData: generatePercentageData(12),
  segmentKeys: ["running", "idle", "maintenance", "offline"],
  percentageDisplay: true,
  showTooltip: true,
  segmentsLabel: "Status Distribution",
  units: "%",
  xAxisTickInterval: 1,
};

export const OnlineOffline = (props: StoryType) => {
  return (
    <StoryContainer>
      <SegmentedBarChart {...props} />
    </StoryContainer>
  );
};

OnlineOffline.args = {
  chartData: generateOnlineOfflineData(12),
  segmentKeys: ["online", "offline"],
  colorMap: {
    online: "--color-core-primary-fill",
    offline: "--color-intent-critical-fill",
  },
  percentageDisplay: true,
  showTooltip: true,
  toolTipKey: "online",
  segmentsLabel: "Device Status",
  units: "%",
  xAxisTickInterval: 2,
};

export const WithAnimation = (props: StoryType) => {
  return (
    <StoryContainer>
      <SegmentedBarChart {...props} />
    </StoryContainer>
  );
};

WithAnimation.args = {
  chartData: generateMockData(12),
  segmentKeys: ["active", "inactive", "error"],
  showTooltip: true,
  animate: true,
  segmentsLabel: "Animated Chart",
  units: " TH/S",
  yAxisPadding: 0.15,
  xAxisTickInterval: 1,
};

WithAnimation.parameters = {
  docs: {
    description: {
      story:
        "Example with animation enabled. Bars will animate from bottom to top on initial render.",
    },
  },
};

export const WithCustomXAxisPadding = (props: StoryType) => {
  return (
    <StoryContainer>
      <SegmentedBarChart {...props} />
    </StoryContainer>
  );
};

WithCustomXAxisPadding.args = {
  chartData: generateMockData(12),
  segmentKeys: ["active", "inactive", "error"],
  showTooltip: true,
  segmentsLabel: "Custom X-Axis Padding",
  units: " TH/S",
  xAxisPadding: 12, // Creates spacing by adding padding to the X-axis
  barWidth: 20,
  yAxisPadding: 0.15,
  xAxisTickInterval: 1,
};

WithCustomXAxisPadding.parameters = {
  docs: {
    description: {
      story:
        "Example with custom xAxisPadding of 12px. This controls the spacing between bars by adjusting the padding on the X-axis, which works with linear/time scales.",
    },
  },
};

// Export default meta for Storybook
export default {
  title: "Shared/SegmentedBarChart",
  component: SegmentedBarChart,
  tags: ["autodocs"],
  parameters: {
    docs: {
      description: {
        component:
          "A segmented bar chart component that displays stacked data with customizable colors, tooltips, and percentage display mode. Supports animation, custom segment colors, and selective tooltip display for specific segments.",
      },
    },
  },
  argTypes: {
    chartData: {
      description: "Array of data points with datetime and segment values",
      control: { type: "object" },
    },
    segmentKeys: {
      description: "Array of keys to extract from chartData for each segment",
      control: { type: "array" },
    },
    colorMap: {
      description: "Optional mapping of segment keys to CSS color variables",
      control: { type: "object" },
    },
    units: {
      description:
        "Units to display in tooltips (e.g., ' TH/s', '%', ' devices')",
      control: { type: "text" },
    },
    percentageDisplay: {
      description: "When true, displays bars as percentages with full height",
      control: { type: "boolean" },
    },
    segmentsLabel: {
      description: "Label for the segments (currently unused)",
      control: { type: "text" },
    },
    showTooltip: {
      description: "Whether to show tooltips on hover",
      control: { type: "boolean" },
    },
    animate: {
      description: "Whether to animate bars on initial render (default: false)",
      control: { type: "boolean" },
      defaultValue: false,
    },
    className: {
      description: "Additional CSS classes for the container",
      control: { type: "text" },
    },
    height: {
      description: "Height of the chart in pixels",
      control: { type: "number" },
    },
    barWidth: {
      description: "Width of each bar in pixels",
      control: { type: "number" },
    },
    barGap: {
      description:
        "Gap between bars in pixels (Note: only works with categorical scales)",
      control: { type: "number" },
    },
    xAxisPadding: {
      description:
        "Padding for X-axis in pixels (controls spacing between bars)",
      control: { type: "number" },
    },
    yAxisPadding: {
      description:
        "Percentage to extend Y-axis above max value (e.g., 0.1 = 10%)",
      control: { type: "number" },
    },
    yAxisTickCount: {
      description: "Number of horizontal grid lines/ticks",
      control: { type: "number" },
    },
    xAxisTickInterval: {
      description: "Show tick every N bars (1 = show all)",
      control: { type: "number" },
    },
    toolTipKey: {
      description:
        "Key to display in tooltip, null to hide tooltip, undefined for total",
      control: { type: "text" },
    },
  },
};
