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

import TickTooltip from "../common/TickTooltip";
import { chartData } from "./constants";

const EfficiencyChart = () => {
  const {
    tooltipData,
    setTooltipData,
    isTooltipActive,
    setTooltipActive,
    tooltipRef,
  } = useTooltip();

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
          verticalPoints={[42.5, 100, 150, 200, 250, 300, 350, 400, 450, 500]}
          horizontalPoints={[30, 70, 110, 150, 192.5]}
        />
        <XAxis {...xAxisProps} />
        <YAxis {...yAxisProps} padding={{ top: -5, bottom: 20 }} />
        <Tooltip
          active={isTooltipActive}
          position={{ y: tooltipData.y - 33, x: tooltipData.x - 112 }}
          content={
            <TickTooltip
              onClick={setTooltipData}
              tooltipData={tooltipData}
              unit="J/TH"
            />
          }
          trigger="click"
          cursor={<LineCursor />}
        />
        <Line
          type="basis"
          dataKey="value"
          stroke="#90C300"
          strokeWidth={2.5}
          label={false}
          dot={false}
          strokeLinecap="round"
          strokeLinejoin="round"
          className="hover:cursor-pointer"
          activeDot={
            tooltipData.payload.length ? <LineDot color="#90C300" /> : <></>
          }
        />
      </LineChart>
    </ChartWrapper>
  );
};

export default EfficiencyChart;
