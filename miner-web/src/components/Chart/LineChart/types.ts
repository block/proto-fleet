export interface Line {
  dataKey: string;
  stroke: string;
  strokeWidth: number;
}

export interface Data {
  time: string;
  [key: string]: number | string;
}
