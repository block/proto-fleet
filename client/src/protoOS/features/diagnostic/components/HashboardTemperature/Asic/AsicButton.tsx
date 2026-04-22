import { Dispatch, SetStateAction, useMemo } from "react";
import clsx from "clsx";

import { useAsicMetric } from "../AsicMetricContext";
import AsicPopover from "./AsicPopover";
import { getAsicUniqueId } from "./utility";
import { useAsicColor } from "@/protoOS/features/kpis/hooks";
import {
  AsicData,
  convertAndFormatMeasurement,
  getAsicName,
  type Measurement,
  useTemperatureUnit,
} from "@/protoOS/store";
import { usePopover } from "@/shared/components/Popover";

interface AsicButtonProps {
  asic: AsicData;
  hashboardSerial: string;
  showPopover: string | undefined;
  setShowPopover: Dispatch<SetStateAction<string | undefined>>;
  totalAsicCount: number; // Pass this in to avoid calling useMinerHashboard
}

const AsicButton = ({ asic, hashboardSerial, showPopover, setShowPopover, totalAsicCount }: AsicButtonProps) => {
  const { triggerRef: asicRef } = usePopover();
  const { selectedMetric } = useAsicMetric();
  const temperatureUnit = useTemperatureUnit();

  const currentAsicId = useMemo(
    () => (asic.index !== undefined ? getAsicUniqueId(asic.index, hashboardSerial) : undefined),
    [asic.index, hashboardSerial],
  );

  const shouldShowPopover = currentAsicId !== undefined && showPopover === currentAsicId;

  const backgroundColor = useAsicColor(asic);
  const asicName = useMemo(() => {
    return asic.index !== undefined ? getAsicName(totalAsicCount, asic.index) : "";
  }, [totalAsicCount, asic.index]);

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
      className={clsx("relative mb-1.5 grow basis-0 rounded-xl p-[2px] shadow-[0_0_0_3px] phone:truncate", {
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
        className="asic-button w-full cursor-default truncate rounded-lg border border-border-5 text-center font-mono text-mono-text-50 text-text-primary"
      >
        <div className="bg-transparent hover:bg-surface-overlay">
          <div className="flex flex-col items-center gap-1 px-1 py-3">
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
