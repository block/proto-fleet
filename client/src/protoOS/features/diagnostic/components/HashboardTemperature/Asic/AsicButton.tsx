import { Dispatch, SetStateAction, useMemo } from "react";
import clsx from "clsx";

import { useAsicMetric } from "../AsicMetricContext";
import AsicPopover from "./AsicPopover";
import { getAsicUniqueId } from "./utility";
import { useHashboardLayout } from "@/protoOS/features/diagnostic/hashboardLayout";
import { AsicData, convertAndFormatMeasurement, type Measurement, useTemperatureUnit } from "@/protoOS/store";
import { usePopover } from "@/shared/components/Popover";

interface AsicButtonProps {
  asic: AsicData;
  backgroundColor: string; // Resolved by AsicTable so the theme palette is read once per grid
  hashboardSerial: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
  totalAsicCount: number; // Pass this in to avoid calling useMinerHashboard
}

const AsicButton = ({
  asic,
  backgroundColor,
  hashboardSerial,
  showPopover,
  setShowPopover,
  totalAsicCount,
}: AsicButtonProps) => {
  const { triggerRef: asicRef } = usePopover();
  const { selectedMetric } = useAsicMetric();
  const temperatureUnit = useTemperatureUnit();
  const layout = useHashboardLayout();

  const currentAsicId = useMemo(
    () => (asic.index !== undefined ? getAsicUniqueId(asic.index, hashboardSerial) : undefined),
    [asic.index, hashboardSerial],
  );

  const shouldShowPopover = currentAsicId !== undefined && showPopover === currentAsicId;

  const asicName = layout.labelCell(asic, totalAsicCount);

  const metricMeasurement = useMemo((): Measurement | undefined => {
    switch (selectedMetric) {
      case "temperature":
        return asic.temperature?.latest;
      case "hashrate":
        return asic.hashrate?.latest;
      case "voltage":
        return asic.voltage?.latest;
      case "frequency":
        return asic.frequency?.latest;
      default:
        return undefined;
    }
  }, [selectedMetric, asic.temperature, asic.hashrate, asic.voltage, asic.frequency]);

  return (
    <div
      className={clsx("relative rounded-xl shadow-[0_0_0_3px]", layout.cell.frame, {
        "shadow-transparent": !shouldShowPopover,
        "shadow-intent-info-fill": shouldShowPopover,
      })}
      ref={asicRef}
    >
      {shouldShowPopover ? (
        <AsicPopover
          asic={asic}
          closePopover={() => setShowPopover(undefined)}
          closeIgnoreSelectors={[".asic-button"]}
        />
      ) : null}
      <button
        style={{ backgroundColor }}
        className={clsx(
          "asic-button w-full cursor-default truncate rounded-lg border border-border-5 text-center font-mono text-mono-text-50 text-text-primary",
          layout.cell.fill,
        )}
      >
        <div className={clsx("bg-transparent hover:bg-surface-overlay", layout.cell.fill)}>
          <div className={clsx("flex flex-col items-center gap-1 px-1", layout.cell.content)}>
            <div className="text-text-primary-50">{asicName}</div>
            {renderMetricValue()}
          </div>
        </div>
      </button>
    </div>
  );

  function renderMetricValue() {
    const formatMetricDisplay = (value: string) => (
      <div className="text-mono-text-100 font-mono text-text-primary">{value}</div>
    );

    if (!metricMeasurement) {
      return formatMetricDisplay("--");
    }

    if (selectedMetric === "temperature") {
      const formatted = convertAndFormatMeasurement(metricMeasurement, temperatureUnit, false);
      return formatMetricDisplay(formatted || "--");
    }

    if (selectedMetric === "hashrate") {
      const formatted = convertAndFormatMeasurement(metricMeasurement, "GH/s", false);
      return formatMetricDisplay(formatted || "--");
    }

    if (selectedMetric === "voltage") {
      const formatted = convertAndFormatMeasurement(metricMeasurement, "mV", false);
      return formatMetricDisplay(formatted || "--");
    }

    if (selectedMetric === "frequency") {
      const formatted = convertAndFormatMeasurement(metricMeasurement, "MHz", false);
      return formatMetricDisplay(formatted || "--");
    }

    return formatMetricDisplay("--");
  }
};

export default AsicButton;
