import type { Meta, StoryObj } from "@storybook/react";
import ChartWidget from "./ChartWidget";
import LineChart from "@/protoFleet/components/LineChart";
import { ChartData } from "@/shared/components/LineChart/types";

// Generate sample chart data
const generateSampleData = (): ChartData[] => {
  const now = Date.now();
  const data: ChartData[] = [];
  const baseValue = 450;

  // Generate 24 hours of data points (every hour)
  for (let i = 0; i < 24; i++) {
    const timestamp = now - (23 - i) * 60 * 60 * 1000;
    const variation = Math.sin(i / 3) * 50 + Math.random() * 20;

    data.push({
      datetime: timestamp,
      totalHashrate: baseValue + variation,
    });
  }

  return data;
};

const meta: Meta<typeof ChartWidget> = {
  title: "Proto Fleet/Dashboard/ChartWidget",
  component: ChartWidget,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "ChartWidget is a container component that displays a title, optional stats, and chart content. It can display a single stat or multiple stats in a grid layout.",
      },
    },
  },
  tags: ["autodocs"],
  argTypes: {
    statsSize: {
      control: "select",
      options: ["small", "medium", "large"],
      description: "Size of the stats display",
    },
    statsGrid: {
      control: "text",
      description: "Tailwind grid class for stats layout",
    },
    className: {
      control: "text",
      description: "Additional CSS classes",
    },
  },
  decorators: [
    (Story) => (
      <div className="w-[800px] bg-core-primary-5 p-10">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof ChartWidget>;

export const SingleStat: Story = {
  args: {
    stats: {
      label: "Current",
      value: "230.2",
      units: "TH/s",
    },
  },
  render: (args) => {
    const sampleData = generateSampleData();
    return (
      <ChartWidget {...args}>
        <LineChart
          chartData={sampleData}
          aggregateKey="totalHashrate"
          activeKeys={["totalHashrate"]}
          colorMap={{ totalHashrate: "--color-core-primary-fill" }}
          units="TH/s"
        />
      </ChartWidget>
    );
  },
};

export const MultipleStats: Story = {
  args: {
    stats: [
      {
        label: "Hashrate",
        value: "230.2",
        units: "TH/s",
      },
      {
        label: "Efficiency",
        value: "22.5",
        units: "J/TH",
      },
      {
        label: "Temperature",
        value: "65°C",
        units: "Average",
      },
    ],
    statsGrid: "grid-cols-3",
    statsGap: "gap-x-8",
  },
  render: (args) => {
    const sampleData = generateSampleData();
    return (
      <ChartWidget {...args}>
        <LineChart
          chartData={sampleData}
          aggregateKey="totalHashrate"
          activeKeys={["totalHashrate"]}
          colorMap={{ totalHashrate: "--color-core-primary-fill" }}
          units="TH/s"
        />
      </ChartWidget>
    );
  },
};

export const NoStats: Story = {
  args: {},
  render: (args) => {
    const sampleData = generateSampleData();
    return (
      <ChartWidget {...args}>
        <LineChart
          chartData={sampleData}
          aggregateKey="totalHashrate"
          activeKeys={["totalHashrate"]}
          colorMap={{ totalHashrate: "--color-core-primary-fill" }}
          units="TH/s"
        />
      </ChartWidget>
    );
  },
};

export const PercentageStats: Story = {
  args: {
    stats: [
      {
        label: "Overall",
        value: "85%",
        units: "Utilization",
      },
      {
        label: "Active",
        value: "178",
        units: "miners",
      },
      {
        label: "Offline",
        value: "22",
        units: "miners",
      },
    ],
    statsGrid: "grid-cols-3",
    statsSize: "medium",
  },
  render: (args) => {
    const sampleData = generateSampleData();
    return (
      <ChartWidget {...args}>
        <LineChart
          chartData={sampleData}
          aggregateKey="totalHashrate"
          activeKeys={["totalHashrate"]}
          colorMap={{ totalHashrate: "--color-core-primary-fill" }}
          units="%"
        />
      </ChartWidget>
    );
  },
};

export const SmallStats: Story = {
  args: {
    stats: [
      {
        label: "Min",
        value: "220.1",
        units: "TH/s",
      },
      {
        label: "Avg",
        value: "230.2",
        units: "TH/s",
      },
      {
        label: "Max",
        value: "245.8",
        units: "TH/s",
      },
    ],
    statsGrid: "grid-cols-3",
    statsSize: "small",
  },
  render: (args) => {
    const sampleData = generateSampleData();
    return (
      <ChartWidget {...args}>
        <LineChart
          chartData={sampleData}
          aggregateKey="totalHashrate"
          activeKeys={["totalHashrate"]}
          colorMap={{ totalHashrate: "--color-core-primary-fill" }}
          units="TH/s"
        />
      </ChartWidget>
    );
  },
};
