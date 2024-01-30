import { Data } from "./LineChart";

interface LineLabelProps {
  chartData: Data[];
  index?: number;
  lineIndex: number;
  text: string;
  value?: number;
  x?: number;
  y?: number;
}

interface Line {
  value?: number;
  y: number;
  label: string;
}

let lines: Line[] = [];

const LineLabel = ({
  chartData,
  index,
  lineIndex,
  text,
  value,
  x = 0,
  y = 0,
}: LineLabelProps) => {
  // only show label on the last data point
  if (index === chartData.length - 1) {
    // store the last y value of all the lines before rendering any of their labels
    lines.push({ label: text, value, y });

    // once we are on the last line, sort the lines by y value and render them
    // don't count "time" key
    if (lineIndex === Object.keys(chartData[0]).length - 2) {
      const sortedLines = lines.sort((a, b) => b.y - a.y);
      // clear out lines for the next render
      lines = [];

      // make sure there is at least 25px between each label
      let min = sortedLines[0].y + 25;
      sortedLines.forEach((line) => {
        line.y = min = Math.min(min - 25, line.y);
      });

      return sortedLines.map(({ label, value, y }) => (
        <text key={label}>
          <tspan
            x={x}
            y={y}
            dx={20}
            fill="#96969D"
            fontSize={9}
            fontFamily="Inter"
            fontWeight={600}
          >
            {label.toUpperCase()}
          </tspan>
          <tspan
            x={x}
            y={y + 15}
            dx={20}
            fill="#000"
            fontSize={14}
            fontFamily="Inter"
            fontWeight={500}
          >
            {value}
          </tspan>
        </text>
      ));
    }
  }
  return null;
};

export default LineLabel;
