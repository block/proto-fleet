import { useMemo, useState } from "react";
import { Line, LineChart, Tooltip, XAxis } from "recharts";

import {
  ChartWrapper,
  LineCursor,
  LineDot,
  TimeXAxisTick,
  xAxisProps,
} from "components/Chart";

import { NullLineProps } from "pages/Home/Hashrate/HashrateChart/constants";

import AsicChartTooltip, { TooltipData } from "./AsicChartTooltip";
import { hashrateLineProps, temperatureLineProps } from "./constants";
import { ChartData } from "./types";
import { getChartData } from "./utility";

interface AsicChartProps {
  hashrateData: ChartData[];
  temperatureData: ChartData[];
}

const AsicChart = ({ hashrateData, temperatureData }: AsicChartProps) => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  const chartData = useMemo(() => {
    return getChartData({ hashrateData, temperatureData });
  }, [hashrateData, temperatureData]);

  return (
    <ChartWrapper>
      <LineChart
        data={chartData}
        margin={{
          top: 10,
          right: 12,
          left: 12,
          bottom: 0,
        }}
      >
        <XAxis
          {...xAxisProps}
          tick={
            <TimeXAxisTick
              tooltipDatetime={tooltipData.payload[0]?.payload.datetime}
              dataPointCount={hashrateData?.length}
              maxTicksToShow={5}
              minXPosition={60}
              maxXPosition={220}
            />
          }
        />
        <Tooltip
          position={{ y: tooltipData.y - 150, x: tooltipData.x - 90 }}
          content={
            <AsicChartTooltip
              onHover={setTooltipData}
              tooltipData={tooltipData}
            />
          }
          cursor={<LineCursor />}
          isAnimationActive={false}
        />
        <Line
          {...hashrateLineProps}
          {...NullLineProps}
          activeDot={<></>}
          strokeOpacity={0.5}
        />
        <Line
          {...hashrateLineProps}
          activeDot={
            tooltipData.payload[0]?.payload.hashrate_ghs !== undefined ? (
              <LineDot color="#000" />
            ) : (
              <></>
            )
          }
        />
        <Line
          {...temperatureLineProps}
          {...NullLineProps}
          activeDot={<></>}
          strokeOpacity={0.5}
        />
        <Line
          {...temperatureLineProps}
          activeDot={
            tooltipData.payload[0]?.payload.temp_c !== undefined ? (
              <LineDot color="#FF5B00" />
            ) : (
              <></>
            )
          }
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default AsicChart;
