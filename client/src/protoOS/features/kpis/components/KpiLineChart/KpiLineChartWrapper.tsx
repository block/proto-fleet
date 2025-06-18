import useHashboardLocationStore from "@/protoOS/store/useHashboardLocationStore";
import KpiChart, {
  type HashboardLocationStore,
  type KpiChartProps,
  type TimeSeriesWithSerial,
} from "@/shared/features/kpis/components/KpiLineChart";
import { getHashboardColor } from "@/shared/features/kpis/components/KpiLineChart/utility";

// Wrapper component for ProtoOS that uses the shared KpiLineChart component
const KpiLineChartWrapper = (
  props: Omit<KpiChartProps, "hashboardLocationStore" | "getHashboardColorMap">,
) => {
  const getSlotByHbSn = useHashboardLocationStore(
    (state) => state.getSlotByHbSn,
  );
  const getBayByHbSn = useHashboardLocationStore((state) => state.getBayByHbSn);
  const getBayCount = useHashboardLocationStore((state) => state.getBayCount);
  const getBaySlotIndexByHbSn = useHashboardLocationStore(
    (state) => state.getBaySlotIndexByHbSn,
  );

  // Create location provider for ProtoOS
  const hashboardLocationStore: HashboardLocationStore = {
    getSlotByHbSn: (serial) => getSlotByHbSn(serial) ?? null,
    getBayByHbSn: (serial) => getBayByHbSn(serial) ?? null,
    getBayCount,
    getBaySlotIndexByHbSn: (serial) => getBaySlotIndexByHbSn(serial) ?? 1,
  };

  // Create a function to generate the hashboard color map for ProtoOS
  const getHashboardColorMap = (series: TimeSeriesWithSerial[]) => {
    type HbColorMap = {
      [key: string]: {
        line: string;
        text: string;
      };
    };

    return series.reduce((acc, { serial }) => {
      acc[serial] = getHashboardColor(
        getSlotByHbSn(serial) ?? 1,
        getBayByHbSn(serial) ?? 1,
        getBaySlotIndexByHbSn(serial) ?? 1,
        getBayCount(),
      );

      return acc;
    }, {} as HbColorMap);
  };

  return (
    <KpiChart
      {...props}
      hashboardLocationStore={hashboardLocationStore}
      getHashboardColorMap={getHashboardColorMap}
    />
  );
};

export default KpiLineChartWrapper;
