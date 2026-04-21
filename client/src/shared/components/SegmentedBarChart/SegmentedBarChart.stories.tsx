import type { Meta, StoryObj } from "@storybook/react";
import SegmentedBarChart from "./SegmentedBarChart";
import type { SegmentedBarChartData } from "./types";

const meta = {
  title: "shared/SegmentedBarChart",
  component: SegmentedBarChart,
  parameters: {
    layout: "centered",
  },
  decorators: [
    (Story) => (
      <div style={{ width: "800px", padding: "2rem", backgroundColor: "var(--color-surface-base)" }}>
        <Story />
      </div>
    ),
  ],
  tags: ["autodocs"],
  argTypes: {
    barWidth: {
      control: { type: "number", min: 4, max: 20, step: 1 },
      description: "Width of each bar in pixels",
    },
    height: {
      control: { type: "number", min: 100, max: 400, step: 20 },
      description: "Height of the chart",
    },
    percentageDisplay: {
      control: "boolean",
      description: "Display values as percentages",
    },
    showDateLabel: {
      control: "boolean",
      description: "Show date instead of time on X-axis",
    },
  },
} satisfies Meta<typeof SegmentedBarChart>;

export default meta;
type Story = StoryObj<typeof meta>;

// Generate sample data
const generateData = (points: number, baseTime: number = Date.now()): SegmentedBarChartData[] => {
  const data: SegmentedBarChartData[] = [];
  for (let i = 0; i < points; i++) {
    const normal = 60 + Math.random() * 30;
    const hot = Math.random() * 5;
    const cold = Math.random() * 3;
    const critical = Math.random() * 2;

    data.push({
      datetime: baseTime + i * 3600000, // 1 hour intervals
      normal: Math.round(normal),
      hot: Math.round(hot),
      cold: Math.round(cold),
      critical: Math.round(critical),
    });
  }
  return data;
};

// Temperature color map
const temperatureColorMap = {
  normal: "var(--color-extended-navy-fill)",
  hot: "var(--color-surface-10)",
  cold: "var(--color-intent-warning-fill)",
  critical: "var(--color-intent-critical-fill)",
};

export const Default: Story = {
  args: {
    chartData: generateData(24),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    height: 200,
    percentageDisplay: false,
    showDateLabel: false,
    xAxisTickInterval: 2,
  },
};

export const PercentageDisplay: Story = {
  args: {
    ...Default.args,
    percentageDisplay: true,
    units: "%",
  },
};

export const FewBars: Story = {
  name: "Few Bars (2 bars with 4px gap)",
  args: {
    chartData: generateData(2),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    barWidth: 8,
    height: 200,
    percentageDisplay: true,
    showDateLabel: false,
  },
};

export const MediumBars: Story = {
  name: "Medium Bars (8 bars with 4px gaps)",
  args: {
    chartData: generateData(8),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    barWidth: 8,
    height: 200,
    percentageDisplay: true,
    showDateLabel: false,
  },
};

export const ManyBars: Story = {
  name: "Many Bars (24 bars with 4px gaps)",
  args: {
    chartData: generateData(24),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    barWidth: 8,
    height: 200,
    percentageDisplay: true,
    showDateLabel: false,
  },
};

export const WithDateLabel: Story = {
  args: {
    ...Default.args,
    showDateLabel: true,
    chartData: generateData(7, Date.now() - 3 * 24 * 3600000), // 3 days ago
  },
};

export const CustomBarWidth: Story = {
  name: "Custom Bar Width (12px bars)",
  args: {
    ...Default.args,
    barWidth: 12,
    chartData: generateData(16),
  },
};

export const NoData: Story = {
  args: {
    ...Default.args,
    chartData: null,
  },
};

export const MultipleCharts: Story = {
  name: "Multiple Charts Side by Side",
  args: {
    chartData: generateData(8),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    barWidth: 8,
    height: 200,
    percentageDisplay: true,
  },
  render: () => {
    const chart1Data = generateData(2);
    const chart2Data = generateData(8);
    const chart3Data = generateData(4);

    return (
      <div style={{ display: "flex", gap: "0", width: "100%" }}>
        <div style={{ flex: 1 }}>
          <SegmentedBarChart
            chartData={chart1Data}
            segmentKeys={["normal", "hot", "cold", "critical"]}
            colorMap={temperatureColorMap}
            barWidth={8}
            barGap={8}
            height={200}
            percentageDisplay={true}
            showDateLabel={true}
          />
        </div>
        <div style={{ flex: 1 }}>
          <SegmentedBarChart
            chartData={chart2Data}
            segmentKeys={["normal", "hot", "cold", "critical"]}
            colorMap={temperatureColorMap}
            barWidth={8}
            barGap={8}
            height={200}
            percentageDisplay={true}
            showDateLabel={true}
          />
        </div>
        <div style={{ flex: 1 }}>
          <SegmentedBarChart
            chartData={chart3Data}
            segmentKeys={["normal", "hot", "cold", "critical"]}
            colorMap={temperatureColorMap}
            barWidth={8}
            barGap={8}
            height={200}
            percentageDisplay={true}
            showDateLabel={true}
          />
        </div>
      </div>
    );
  },
};

export const InteractiveSpacing: Story = {
  name: "Interactive Spacing Demo",
  args: {
    chartData: generateData(6),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    barWidth: 8,
    barGap: 4,
    height: 200,
    percentageDisplay: true,
  },
  render: () => {
    const data = generateData(6);

    return (
      <div style={{ padding: "20px" }}>
        <h3 style={{ marginBottom: "20px", color: "var(--color-text-primary)" }}>Exact 4px gaps between 8px bars</h3>
        <div style={{ position: "relative" }}>
          <SegmentedBarChart
            chartData={data}
            segmentKeys={["normal", "hot", "cold", "critical"]}
            colorMap={temperatureColorMap}
            barWidth={8}
            barGap={4}
            height={200}
            percentageDisplay={true}
          />
          {/* Visual guide showing the spacing */}
          <div
            style={{
              position: "absolute",
              bottom: "30px",
              left: "40px",
              right: "20px",
              height: "20px",
              display: "flex",
              gap: "4px",
              alignItems: "center",
              pointerEvents: "none",
            }}
          >
            {data.map((_, i) => (
              <div key={i} style={{ display: "flex", alignItems: "center" }}>
                <div
                  style={{
                    width: "8px",
                    height: "2px",
                    backgroundColor: "var(--color-primary-500)",
                    opacity: 0.5,
                  }}
                />
                {i < data.length - 1 && (
                  <div
                    style={{
                      width: "4px",
                      height: "10px",
                      borderLeft: "1px dashed var(--color-danger-500)",
                      borderRight: "1px dashed var(--color-danger-500)",
                      marginLeft: "0",
                      opacity: 0.5,
                    }}
                  />
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  },
};

export const ResponsiveBarWidth: Story = {
  name: "Responsive Bar Width",
  args: {
    chartData: generateData(12),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    barWidth: {
      phone: 6,
      tablet: 8,
      laptop: 10,
      desktop: 12,
    },
    height: 200,
    percentageDisplay: false,
  },
  render: (args) => (
    <div style={{ padding: "20px" }}>
      <p style={{ marginBottom: "20px", color: "var(--color-text-secondary)" }}>
        Bar width varies by viewport: phone: 6px, tablet: 8px, laptop: 10px, desktop: 12px. Resize your browser window
        to see the responsive behavior.
      </p>
      <SegmentedBarChart {...args} />
    </div>
  ),
};

export const ResponsiveBarGap: Story = {
  name: "Responsive Bar Gap",
  args: {
    chartData: generateData(10),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    barWidth: 8,
    barGap: {
      phone: 2,
      tablet: 4,
      laptop: 6,
      desktop: 8,
    },
    height: 200,
    percentageDisplay: false,
  },
  render: (args) => (
    <div style={{ padding: "20px" }}>
      <p style={{ marginBottom: "20px", color: "var(--color-text-secondary)" }}>
        Bar gap varies by viewport: phone: 2px, tablet: 4px, laptop: 6px, desktop: 8px. Resize your browser window to
        see the responsive behavior.
      </p>
      <SegmentedBarChart {...args} />
    </div>
  ),
};

export const ResponsiveBoth: Story = {
  name: "Responsive Width & Gap",
  args: {
    chartData: generateData(8),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    barWidth: {
      phone: 6,
      tablet: 8,
      laptop: 10,
      desktop: 12,
    },
    barGap: {
      phone: 2,
      tablet: 4,
      laptop: 6,
      desktop: 8,
    },
    height: 200,
    percentageDisplay: true,
  },
  render: (args) => (
    <div style={{ padding: "20px" }}>
      <p style={{ marginBottom: "20px", color: "var(--color-text-secondary)" }}>
        Both bar width and gap are responsive:
        <br />
        Phone: 6px bars with 2px gaps | Tablet: 8px bars with 4px gaps
        <br />
        Laptop: 10px bars with 6px gaps | Desktop: 12px bars with 8px gaps
      </p>
      <SegmentedBarChart {...args} />
    </div>
  ),
};

export const PartialResponsive: Story = {
  name: "Partial Responsive Values",
  args: {
    chartData: generateData(10),
    segmentKeys: ["normal", "hot", "cold", "critical"],
    colorMap: temperatureColorMap,
    units: " miners",
    barWidth: {
      phone: 6,
      desktop: 12,
      // tablet and laptop will inherit from available values
    },
    barGap: 4, // Static gap across all viewports
    height: 200,
    percentageDisplay: false,
  },
  render: (args) => (
    <div style={{ padding: "20px" }}>
      <p style={{ marginBottom: "20px", color: "var(--color-text-secondary)" }}>
        Bar width is responsive with only phone (6px) and desktop (12px) defined.
        <br />
        Tablet and laptop will fall back to the next available value.
        <br />
        Bar gap is static at 4px across all viewports.
      </p>
      <SegmentedBarChart {...args} />
    </div>
  ),
};
