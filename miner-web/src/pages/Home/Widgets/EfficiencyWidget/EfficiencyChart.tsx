import { useCallback, useRef, useState } from "react";

import { useClickOutside } from "common/hooks/useClickOutside";

import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import AxisTick from "../common/AxisTick";
import LineCursor from "../common/LineCursor";
import LineDot from "../common/LineDot";
import TickTooltip, { TooltipData } from "../common/TickTooltip";
import TimeXAxisTick from "../common/TimeXAxisTick";
import { chartData } from "./constants";

const EfficiencyChart = () => {
  const [tooltipData, setTooltipData] = useState<TooltipData>({
    x: 0,
    y: 0,
    payload: [],
  });
  const [isTooltipActive, setTooltipActive] = useState(false);
  const tooltipRef = useRef(null);

  const onClickOutside = useCallback(() => {
    setTooltipActive(false);
    setTooltipData({ x: 0, y: 0, payload: [] });
  }, []);

  useClickOutside({ ref: tooltipRef, onClickOutside });

  return (
    <div ref={tooltipRef} className="flex w-full h-full">
      <ResponsiveContainer
        width="100%"
        height="100%"
      >
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
          <XAxis
            dataKey="time"
            axisLine={false}
            tickLine={false}
            interval={0}
            tick={TimeXAxisTick}
          />
          <YAxis
            axisLine={false}
            tickLine={false}
            interval={0}
            tickMargin={12}
            tick={AxisTick}
            padding={{ top: -5, bottom: 20 }}
          />
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
            type="monotone"
            dataKey="value"
            stroke="#90C300"
            strokeWidth={2}
            label={false}
            dot={false}
            strokeLinecap="round"
            strokeLinejoin="round"
            className="hover:cursor-pointer"
            activeDot={tooltipData.payload.length ? <LineDot /> : <></>}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
};

export default EfficiencyChart;
