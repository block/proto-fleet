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
  getProtoContainerAsicLabel,
  type Measurement,
  useIsProtoContainer,
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
  const isProtoContainer = useIsProtoContainer();

  const currentAsicId = useMemo(
    () => (asic.index !== undefined ? getAsicUniqueId(asic.index, hashboardSerial) : undefined),
    [asic.index, hashboardSerial],
  );

  const shouldShowPopover = currentAsicId !== undefined && showPopover === currentAsicId;

  const backgroundColor = useAsicColor(asic);
  const asicName = useMemo(() => {
    // Proto container modules use the serpentine layout (position-based label);
    // rigs keep the existing index-based A/B split, byte-for-byte.
    if (isProtoContainer) {
      return asic.row !== undefined && asic.column !== undefined
        ? getProtoContainerAsicLabel(asic.row, asic.column)
        : "";
    }
    return asic.index !== undefined ? getAsicName(totalAsicCount, asic.index) : "";
  }, [isProtoContainer, asic.row, asic.column, totalAsicCount, asic.index]);

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
      className={clsx(
        "relative rounded-xl shadow-[0_0_0_3px]",
        isProtoContainer ? "h-14 min-w-14 flex-1 p-0" : "mb-1.5 grow basis-0 p-[2px] phone:truncate",
        {
          "shadow-transparent": !shouldShowPopover,
          "shadow-intent-info-fill": shouldShowPopover,
        },
      )}
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
          { "h-full": isProtoContainer },
        )}
      >
        <div className={clsx("bg-transparent hover:bg-surface-overlay", { "h-full": isProtoContainer })}>
          <div
            className={clsx("flex flex-col items-center gap-1 px-1", {
              "h-full justify-center py-1": isProtoContainer,
              "py-3": !isProtoContainer,
            })}
          >
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
