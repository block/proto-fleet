import { TimeSeriesData } from "@/protoOS/api/types";
import { convertMhSToThS } from "@/shared/utils/utility";

// Legacy function - kept for backwards compatibility
export const convertHashrateValues = (data: TimeSeriesData[]) => {
  return (
    data?.map((hashrate) => ({
      datetime: hashrate.datetime || 0,
      value: convertMhSToThS(hashrate.value) || 0,
    })) || []
  );
};
