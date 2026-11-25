import { type Dispatch, type SetStateAction, useCallback, useEffect, useMemo, useRef } from "react";
import clsx from "clsx";
import { useAsicMetric } from "../AsicMetricContext";
import { groupAsicsByRow } from "../utility";
import AsicButton from "./AsicButton";
import { getMetricColor, getMetricRange } from "./asicColors";
import AsicPopover from "./AsicPopover";
import { formatMetric, getMetricValue, metricLabels } from "./metricDisplay";
import { getAsicUniqueId } from "./utility";
import { useHashboardLayout } from "@/protoOS/features/diagnostic/hashboardLayout";
import { useAsicPalette } from "@/protoOS/features/kpis/hooks";
import { type AsicData, useTemperatureUnit } from "@/protoOS/store";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface AsicTableProps {
  asics: AsicData[];
  hashboardSerialNumber: string;
  pending: boolean;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
}

const AsicGrid = ({ asics, hashboardSerialNumber, pending, showPopover, setShowPopover }: AsicTableProps) => {
  const layout = useHashboardLayout();
  const temperatureUnit = useTemperatureUnit();
  const { selectedMetric } = useAsicMetric();
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const palette = useAsicPalette();
  const previousSerial = useRef(hashboardSerialNumber);

  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);
  useEffect(() => {
    if (previousSerial.current !== hashboardSerialNumber) {
      setShowPopover(undefined);
      previousSerial.current = hashboardSerialNumber;
    }
  }, [hashboardSerialNumber, setShowPopover]);

  const rows = useMemo(() => groupAsicsByRow(asics), [asics]);
  const values = useMemo(
    () => new Map(asics.map((asic) => [asic, getMetricValue(asic, selectedMetric)])),
    [asics, selectedMetric],
  );
  const range = useMemo(() => getMetricRange(selectedMetric, [...values.values()]), [selectedMetric, values]);
  const openAsic = asics.find(
    (asic) => asic.index !== undefined && getAsicUniqueId(asic.index, hashboardSerialNumber) === showPopover,
  );
  const selectAsic = useCallback(
    (asic: AsicData, trigger: HTMLDivElement) => {
      if (asic.index === undefined) return;
      triggerRef.current = trigger;
      const id = getAsicUniqueId(asic.index, hashboardSerialNumber);
      setShowPopover((previous) => (previous === id ? undefined : id));
    },
    [hashboardSerialNumber, setShowPopover, triggerRef],
  );
  const close = useCallback(() => setShowPopover(undefined), [setShowPopover]);

  return (
    <div className="relative mt-6 h-full">
      {pending && !asics.length ? (
        <div className="flex justify-center py-10">
          <ProgressCircular indeterminate />
        </div>
      ) : (
        <>
          <div className={layout.grid.frame}>
            {rows.map(({ row, asics: rowAsics }) => (
              <div className={clsx("flex", layout.grid.row, layout.grid.rowClass(row))} key={`asic-${row}`}>
                {rowAsics.map((asic) => (
                  <AsicButton
                    key={`asic-${asic.row}-${asic.column}`}
                    asic={asic}
                    backgroundColor={getMetricColor(palette, selectedMetric, values.get(asic), range)}
                    isOpen={asic === openAsic}
                    onSelect={selectAsic}
                    totalAsicCount={asics.length}
                    metricValue={formatMetric(selectedMetric, values.get(asic), temperatureUnit, false)}
                    accessibleReading={`${metricLabels[selectedMetric]} ${formatMetric(selectedMetric, values.get(asic), temperatureUnit)}`}
                  />
                ))}
              </div>
            ))}
          </div>
          {openAsic ? (
            <AsicPopover
              key={showPopover}
              asic={openAsic}
              selectedMetric={selectedMetric}
              closePopover={close}
              closeIgnoreSelectors={[".asic-button"]}
            />
          ) : null}
        </>
      )}
    </div>
  );
};

const AsicTable = (props: AsicTableProps) => (
  <PopoverProvider>
    <AsicGrid {...props} />
  </PopoverProvider>
);
export default AsicTable;
