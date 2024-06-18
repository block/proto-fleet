import { useState } from "react";
import { Line, LineChart, Tooltip, XAxis } from "recharts";

import {
  ChartWrapper,
  LineCursor,
  LineDot,
  TimeXAxisTick,
  xAxisProps,
} from "components/Chart";

import AsicChartTooltip, { TooltipData } from "./AsicChartTooltip";
import { ChartData } from "./types";

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

  const onHoverChart = (data: TooltipData) => {
    const newTooltipData = data.payload.length ? {
      ...data,
      payload: [
        {
          payload: {
            time: data.payload[0].payload.time,
            temp_c: data.payload[0].payload.value,
            hashrate_ghs: data.payload[1].payload.value,
          },
        },
      ],
    } : data;
    setTooltipData(newTooltipData);
  };

  return (
    <ChartWrapper>
      <LineChart
        margin={{
          top: 0,
          right: 12,
          left: 12,
          bottom: 0,
        }}
      >
        <XAxis
          {...xAxisProps}
          xAxisId="hashrate"
          tick={
            <TimeXAxisTick
              tooltipTime={tooltipData.payload[0]?.payload.time}
              dataPointCount={hashrateData?.length}
              maxTicksToShow={5}
            />
          }
        />
        <XAxis {...xAxisProps} xAxisId="temperature" hide />
        <Tooltip
          position={{ y: tooltipData.y - 150, x: tooltipData.x - 90 }}
          content={
            <AsicChartTooltip
              onHover={onHoverChart}
              tooltipData={tooltipData}
            />
          }
          cursor={<LineCursor />}
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          data={temperatureData}
          xAxisId="temperature"
          dataKey="value"
          stroke="#FF5B00"
          strokeWidth={2.5}
          label={false}
          dot={false}
          strokeLinecap="round"
          strokeLinejoin="round"
          activeDot={
            tooltipData.payload.length ? <LineDot color="#FF5B00" /> : <></>
          }
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          data={hashrateData}
          xAxisId="hashrate"
          dataKey="value"
          stroke="#000"
          strokeWidth={2.5}
          label={false}
          dot={false}
          strokeLinecap="round"
          strokeLinejoin="round"
          activeDot={
            tooltipData.payload.length ? <LineDot color="#000" /> : <></>
          }
          isAnimationActive={false}
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default AsicChart;
