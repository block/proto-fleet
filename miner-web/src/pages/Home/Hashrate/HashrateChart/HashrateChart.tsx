import { useEffect, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { useWindowDimensions } from "common/hooks/useWindowDimensions";

import {
  ChartWrapper,
  LineCursor,
  LineDot,
  xAxisProps,
  yAxisProps,
} from "components/Chart";

import { chartData, LineProps } from "./constants";
import HashrateTooltip, { TooltipData } from "./HashrateTooltip";
import { getPoint } from "./utility";

const HashrateChart = () => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });
  const [initChart, setInitChart] = useState(false);
  const { isDesktop } = useWindowDimensions();

  const max = Math.max(...chartData.map((data) => data.avgHashrate));
  const nearestTen = Math.round(max / 10) * 10;
  const maxDomain = nearestTen + 20;

  useEffect(() => {
    setTimeout(() => {
      // the chart should only animate on first render
      // animation takes around 1.5s
      setInitChart(true);
    }, 1500);
  }, []);

  const firstVerticalPoint = isDesktop ? 130 : 105;
  const verticalGap = isDesktop ? 90 : 55;
  const verticalPoints = [...Array(9)].map((_, i) =>
    getPoint(i, firstVerticalPoint, verticalGap)
  );

  const firstHorizontalPoint = 40;
  const horizontalGap = 35;
  const horizontalPoints = [...Array(9)].map((_, i) =>
    getPoint(i, firstHorizontalPoint, horizontalGap)
  );

  return (
    <ChartWrapper>
      <LineChart
        data={chartData}
        margin={{
          top: 0,
          right: 0,
          left: -17,
          bottom: 5,
        }}
      >
        <CartesianGrid
          strokeOpacity={0.2}
          color="black"
          verticalPoints={[43, ...verticalPoints]}
          horizontalPoints={[...horizontalPoints, 365]}
        />
        <XAxis {...xAxisProps} tickMargin={12} />
        <YAxis
          {...yAxisProps}
          padding={{ top: -5, bottom: 20 }}
          domain={[0, maxDomain]}
          tickCount={maxDomain / 5 + 2}
        />
        <Tooltip
          position={{ y: tooltipData.y - 150, x: tooltipData.x - 290 }}
          content={
            <HashrateTooltip
              onHover={setTooltipData}
              tooltipData={tooltipData}
            />
          }
          cursor={<LineCursor />}
          isAnimationActive={false}
        />
        {!!tooltipData.payload.length && (
          <>
            <Line
              {...LineProps}
              dataKey="hashrate1"
              stroke="#00A4FB"
              strokeOpacity={0.5}
              isAnimationActive={false}
            />
            <Line
              {...LineProps}
              dataKey="hashrate2"
              stroke="#90C300"
              strokeOpacity={0.5}
              isAnimationActive={false}
            />
            <Line
              {...LineProps}
              dataKey="hashrate3"
              stroke="#783EED"
              strokeOpacity={0.8}
              isAnimationActive={false}
            />
          </>
        )}
        <Line
          {...LineProps}
          dataKey="avgHashrate"
          stroke="#FF7900"
          activeDot={
            tooltipData.payload.length ? <LineDot color="#FF7900" /> : <></>
          }
          isAnimationActive={!initChart}
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default HashrateChart;
