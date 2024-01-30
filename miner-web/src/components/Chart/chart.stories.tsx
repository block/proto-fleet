import { useState } from "react";
import LineChart from "./LineChart";

const time = ["04:00", "08:00", "12:00", "16:00", "20:00", "24:00"];

const pageData = [
  {
    time: time[0],
    "Board 1": 12,
    "Board 2": 6,
    "Board 3": 2,
    Efficiency: 41,
  },
  {
    time: time[1],
    "Board 1": 18,
    "Board 2": 13,
    "Board 3": 8,
    Efficiency: 45,
  },
  {
    time: time[2],
    "Board 1": 12,
    "Board 2": 6,
    "Board 3": 11,
    Efficiency: 43,
  },
  {
    time: time[3],
    "Board 1": 15,
    "Board 2": 7,
    "Board 3": 20,
    Efficiency: 41,
  },
  {
    time: time[4],
    "Board 1": 12,
    "Board 2": 6,
    "Board 3": 9,
    Efficiency: 38,
  },
  {
    time: time[5],
    "Board 1": 18,
    "Board 2": 12,
    "Board 3": 7,
    Efficiency: 45,
  },
];

export const TrendChart = () => {
  const height = 300;
  const width = 500;
  const [data, setData] = useState(pageData);
  const board1 = Math.floor(Math.random() * 21);
  const board2 = Math.floor(Math.random() * 21);
  const board3 = Math.floor(Math.random() * 21);

  setTimeout(() => {
    data.shift();
    setData(
      [...data].concat({
        time:
          data[data.length - 1].time === time[5]
            ? time[0]
            : time[time.indexOf(data[data.length - 1].time) + 1],
        "Board 1": board1,
        "Board 2": board2,
        "Board 3": board3,
        Efficiency: board1 + board2 + board3,
      })
    );
  }, 2000);
  return (
    <div>
      <div className="text-title-1">Trend charts</div>
      <div style={{ height, width }}>
        <LineChart
          data={data}
          lines={[
            {
              dataKey: "Efficiency",
              stroke: "#F46E38",
              strokeWidth: 3,
            },
            {
              dataKey: "Board 1",
              stroke: "#c6c6c6",
              strokeWidth: 2,
            },
            {
              dataKey: "Board 2",
              stroke: "#c6c6c6",
              strokeWidth: 2,
            },
            {
              dataKey: "Board 3",
              stroke: "#c6c6c6",
              strokeWidth: 2,
            },
          ]}
          height={height}
          width={width}
        />
      </div>
    </div>
  );
};

export default {
  component: TrendChart,
  title: "Charts",
};
