import { useEffect, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import {
  ChartWrapper,
  LineCursor,
  LineDot,
  useTooltip,
  xAxisProps,
  yAxisProps,
} from "components/Chart";

import { chartData, LineProps } from "./constants";
import HashrateTooltip from "./HashrateTooltip";

const HashrateChart = () => {
  const {
    tooltipData,
    setTooltipData,
    isTooltipActive,
    setTooltipActive,
    tooltipRef,
  } = useTooltip();
  const [initChart, setInitChart] = useState(false);

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

  return (
    <ChartWrapper tooltipRef={tooltipRef}>
      <LineChart
        data={chartData}
        margin={{
          top: 0,
          right: 30,
          left: -17,
          bottom: 5,
        }}
        onClick={() => setTooltipActive(true)}
      >
        <CartesianGrid
          strokeOpacity={0.2}
          color="black"
          verticalPoints={[43, 130, 210, 290, 370, 450, 530, 610, 690, 770]}
          horizontalPoints={[40, 75, 110, 145, 180, 215, 250, 285, 320, 365]}
        />
        <XAxis {...xAxisProps} tickMargin={12} />
        <YAxis
          {...yAxisProps}
          padding={{ top: -5, bottom: 20 }}
          domain={[0, maxDomain]}
          tickCount={maxDomain / 5 + 2}
        />
        <Tooltip
          active={isTooltipActive}
          position={{ y: tooltipData.y - 150, x: tooltipData.x - 290 }}
          content={
            <HashrateTooltip
              onClick={setTooltipData}
              tooltipData={tooltipData}
            />
          }
          trigger="click"
          cursor={<LineCursor />}
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
          className="hover:cursor-pointer"
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default HashrateChart;
