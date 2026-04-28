import { useMemo } from "react";

import LineChartComponent from "../KpiLineChart";
import { mockHashrateData, mockHashrateData1, mockHashrateData2, mockHashrateData3 } from "./mocks";
import { conversionFns, convertValues, downsample } from "@/protoOS/features/kpis/hooks/utility";

import { Duration } from "@/shared/components/DurationSelector";

interface LineChartProps {
  duration: Duration;
  hashboards: number;
  units?: string;
}

const processData = (data: any, duration: Duration) => {
  const downsampledData = downsample(data, duration);
  return convertValues(downsampledData, conversionFns.hashrate);
};

export const LineChart = ({ duration, hashboards, units }: LineChartProps) => {
  const chartData = useMemo(() => {
    const aggregateData = processData(mockHashrateData.data, duration);
    const hashboardData = [mockHashrateData1, mockHashrateData2, mockHashrateData3]
      .slice(0, hashboards)
      .map((data, index) => ({
        serial: `hb${index}`,
        data: processData(data.data, duration),
      }));

    // Convert to the format expected by KpiLineChart
    return aggregateData.map((point: any, index: number) => {
      const chartPoint: any = {
        datetime: point.datetime,
        miner: point.value,
      };

      // Add hashboard data for this timestamp
      hashboardData.forEach((hb) => {
        if (hb.data[index]) {
          chartPoint[hb.serial] = hb.data[index].value;
        }
      });

      return chartPoint;
    });
  }, [duration, hashboards]);

  const chartLines = useMemo(() => {
    const lines = ["miner"];
    for (let i = 0; i < hashboards; i++) {
      lines.push(`hb${i}`);
    }
    return lines;
  }, [hashboards]);

  return (
    <div className="my-8 flex justify-center">
      <div className="flex h-[486px] w-[928px]">
        <LineChartComponent chartData={chartData} chartLines={chartLines} units={units} />
      </div>
    </div>
  );
};

export default {
  title: "Proto OS/LineChart",
  args: {
    duration: "12h",
    hashboards: 3,
    units: "TH/s",
  },
  argTypes: {
    duration: {
      control: "select",
      options: ["12h", "24h", "48h", "5d"],
    },
    units: {
      control: "select",
      options: ["TH/s", "kW", "J/TH"],
    },
    hashboards: {
      control: { type: "number", min: 0, max: 3 },
    },
  },
};
