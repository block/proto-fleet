import { type TimeSeriesData } from "@/protoOS/api/types";

export type TimeSeries = {
  name: string;
  data: TimeSeriesData[];
};
