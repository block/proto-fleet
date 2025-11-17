import KpiLineChart from "../KpiLineChart/KpiLineChart";
import ChartWidgetComponent from ".";
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

interface ChartWidgetArgs {
  label: string;
  value: string | number;
  units?: string;
}

export const ChartWidget = ({ label, value, units }: ChartWidgetArgs) => {
  const sampleData = generateSampleData();

  return (
    <div className="bg-core-primary-5 p-10">
      <ChartWidgetComponent label={label} value={value} units={units}>
        <KpiLineChart
          chartData={sampleData}
          aggregateKey="totalHashrate"
          activeKeys={["totalHashrate"]}
          colorMap={{ totalHashrate: "--color-core-primary-fill" }}
          units={units}
        />
      </ChartWidgetComponent>
    </div>
  );
};

export default {
  title: "Proto Fleet/ChartWidget",
  args: {
    label: "Hashrate",
    value: "230.2",
    units: "TH/s",
  },
  argTypes: {
    label: {
      control: "text",
    },
    value: {
      control: "text",
    },
    units: {
      control: "text",
    },
  },
};
