import { useState } from "react";
import { Line, LineChart, Tooltip, XAxis } from "recharts";

import {
  ChartWrapper,
  LineCursor,
  LineDot,
  xAxisProps,
} from "components/Chart";

import AsicChartTooltip, { TooltipData } from "./AsicChartTooltip";
import { chartData } from "./constants";

const AsicChart = () => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });

  return (
    <ChartWrapper>
      <LineChart data={chartData}>
        <XAxis {...xAxisProps} />
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
          type="basis"
          dataKey="temp_c"
          stroke="#FF5B00"
          strokeWidth={2.5}
          label={false}
          dot={false}
          strokeLinecap="round"
          strokeLinejoin="round"
          activeDot={
            tooltipData.payload.length ? <LineDot color="#FF5B00" /> : <></>
          }
        />
        <Line
          type="basis"
          dataKey="hashrate_ghs"
          stroke="#000"
          strokeWidth={2.5}
          label={false}
          dot={false}
          strokeLinecap="round"
          strokeLinejoin="round"
          activeDot={
            tooltipData.payload.length ? <LineDot color="#000" /> : <></>
          }
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default AsicChart;
