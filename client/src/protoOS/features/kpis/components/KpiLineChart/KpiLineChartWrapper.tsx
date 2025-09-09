import { useCallback, useState } from "react";
import HashboardSelector from "../HashboardSelector";
import useHashboardLocationStore from "@/protoOS/store/useHashboardLocationStore";
import KpiChart, {
  type HashboardLocationStore,
  type KpiChartProps,
  type TimeSeriesWithSerial,
} from "@/shared/features/kpis/components/KpiLineChart";
import { getHashboardColor } from "@/shared/features/kpis/components/KpiLineChart/utility";

// Wrapper component for ProtoOS that uses the shared KpiLineChart component
const KpiLineChartWrapper = (
  props: Omit<KpiChartProps, "hashboardLocationStore" | "getSeriesColorMap">,
) => {
  const getSlotByHbSn = useHashboardLocationStore(
    (state) => state.getSlotByHbSn,
  );
  const getBayByHbSn = useHashboardLocationStore((state) => state.getBayByHbSn);
  const getBayCount = useHashboardLocationStore((state) => state.getBayCount);
  const getBaySlotIndexByHbSn = useHashboardLocationStore(
    (state) => state.getBaySlotIndexByHbSn,
  );

  const [showAggregate, setShowAggregate] = useState(true);
  const [activeHashboards, setActiveHashboards] = useState<string[]>(
    props.series.map((s) => s.serial),
  );

  const hashboardLocationStore: HashboardLocationStore = {
    getSlotByHbSn: (serial) => getSlotByHbSn(serial) ?? null,
    getBayByHbSn: (serial) => getBayByHbSn(serial) ?? null,
    getBayCount,
    getBaySlotIndexByHbSn: (serial) => getBaySlotIndexByHbSn(serial) ?? 1,
  };

  const getHashboardColorMap = useCallback(
    (series: TimeSeriesWithSerial[]) => {
      return series.reduce(
        (acc, { serial }) => {
          acc[serial] = getHashboardColor(getSlotByHbSn(serial) ?? 1);

          return acc;
        },
        {} as { [key: string]: string },
      );
    },
    [getSlotByHbSn],
  );

  return (
    <>
      <div className="scrollbar-hide w-[calc(100%+theme(space.28))] -translate-x-14 overflow-x-auto phone:w-[calc(100%+theme(space.12))] phone:-translate-x-6 tablet:w-[calc(100%+theme(space.20))] tablet:-translate-x-10">
        <HashboardSelector
          series={props.series}
          hashboardLocationStore={hashboardLocationStore}
          setActiveHashboards={setActiveHashboards}
          activeHashboards={activeHashboards}
          showAggregate={showAggregate}
          setShowAggregate={setShowAggregate}
          className={"px-14 phone:px-6 tablet:px-10"}
        />
      </div>
      <KpiChart
        {...props}
        showAggregate={showAggregate}
        activeSeries={activeHashboards}
        hashboardLocationStore={hashboardLocationStore}
        getSeriesColorMap={getHashboardColorMap}
      />
    </>
  );
};

export default KpiLineChartWrapper;
