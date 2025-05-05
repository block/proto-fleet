import { useMemo } from "react";

import {
  convertionFns,
  convertValues,
  downsample,
} from "../../../hooks/utility";

import KpiLineChartComponent from "../KpiLineChart";
import {
  mockHashrateData,
  mockHashrateData1,
  mockHashrateData2,
  mockHashrateData3,
} from "./mocks";

import { Duration } from "@/shared/components/DurationSelector";

interface KpiLineChartProps {
  duration: Duration;
  hashboards: number;
  units?: string;
}

const processData = (data: any, duration: Duration) => {
  const downsampledData = downsample(data, duration);
  return convertValues(downsampledData, convertionFns.hashrate);
};

export const KpiLineChart = ({
  duration,
  hashboards,
  units,
}: KpiLineChartProps) => {
  const aggregateSeriesData = useMemo(() => {
    return {
      name: "Total Data",
      data: processData(mockHashrateData.data, duration),
    };
  }, [duration]);

  const seriesData = useMemo(() => {
    let sd = [mockHashrateData1, mockHashrateData2, mockHashrateData3].map(
      (data, index) => ({
        name: "Hashboard Data " + (index + 1),
        data: processData(data.data, duration),
        serial: index.toString(),
      }),
    );

    return sd.slice(0, hashboards);
  }, [hashboards, duration]);

  return (
    <div className="my-8 flex justify-center">
      <div className="flex h-[486px] w-[928px]">
        <KpiLineChartComponent
          aggregateSeries={aggregateSeriesData}
          series={seriesData}
          units={units}
        />
      </div>
    </div>
  );
};

export default {
  title: "protoOS/KpiLineChart",
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
