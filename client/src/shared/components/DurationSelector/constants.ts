import { TimeSeriesDuration } from "@/protoOS/api/generatedApi";

export const durations = [
  TimeSeriesDuration.Value1H,
  TimeSeriesDuration.Value12H,
  TimeSeriesDuration.Value24H,
  TimeSeriesDuration.Value48H,
  TimeSeriesDuration.Value5D,
] as const;
