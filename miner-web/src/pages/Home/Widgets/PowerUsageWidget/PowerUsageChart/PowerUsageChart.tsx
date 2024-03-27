import { useEffect, useState } from "react";
import { Bar, BarChart, Tooltip, XAxis, YAxis } from "recharts";

import { deepClone } from "common/utils/utility";

import {
  ChartWrapper,
  useTooltip,
  xAxisProps,
  yAxisProps,
} from "components/Chart";

import TickTooltip from "../../common/TickTooltip";
import { chartData, marginValue } from "./constants";
import PowerUsageBar from "./PowerUsageBar";

interface chartDataProps {
  time?: string;
  value: number;
}

const PowerUsageChart = () => {
  const {
    tooltipData,
    setTooltipData,
    isTooltipActive,
    setTooltipActive,
    tooltipRef,
  } = useTooltip();

  // TODO: get chart data from API when available
  const [chartDataPadded, setChartDataPadded] = useState<chartDataProps[] | []>(
    []
  );

  useEffect(() => {
    const newData = deepClone(chartData);
    setChartDataPadded(
      newData.map((data: chartDataProps) => {
        data.value += marginValue;
        return data;
      })
    );
  }, []);

  const maxValue = Math.max(...chartData.map((data) => data.value));

  return (
    <ChartWrapper tooltipRef={tooltipRef}>
      <BarChart
        data={chartDataPadded}
        margin={{
          top: 16,
          right: 0,
          left: -34,
          bottom: 0,
        }}
        onClick={() => setTooltipActive(true)}
      >
        <XAxis {...xAxisProps} />
        <YAxis
          {...yAxisProps}
          scale="linear"
          tickMargin={6}
          domain={[0, maxValue + 1]}
          padding={{ top: -26, bottom: 25 }}
        />
        <Tooltip
          active={isTooltipActive}
          cursor={{ fill: "#fff" }}
          position={{ y: -75, x: tooltipData.x - 50 }}
          content={
            <TickTooltip
              onClick={setTooltipData}
              tooltipData={tooltipData}
              marginValue={marginValue}
              unit="kW"
            />
          }
          trigger="click"
        />
        <Bar
          dataKey="value"
          barSize={16}
          radius={[0, 0, 4, 4]}
          shape={<PowerUsageBar />}
          activeBar={<PowerUsageBar active={!!tooltipData.payload.length} />}
          className="hover:cursor-pointer"
        />
      </BarChart>
    </ChartWrapper>
  );
};

export default PowerUsageChart;
