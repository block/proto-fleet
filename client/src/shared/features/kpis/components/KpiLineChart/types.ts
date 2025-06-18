import { TimeSeriesDataPoint } from "@/shared/features/kpis";

export type TimeSeries = {
  name: string;
  data: TimeSeriesDataPoint[];
};

export type TimeSeriesWithSerial = TimeSeries & {
  serial: string;
};
