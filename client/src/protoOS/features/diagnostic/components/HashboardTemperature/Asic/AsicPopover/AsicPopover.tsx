import { useMemo } from "react";
import AsicChart from "./AsicChart";
import AsicPopoverRow from "./AsicPopoverRow";
import { convertTelemetryHashrateToChartData, convertTelemetryTemperatureToChartData } from "./utility";
import { AsicData, convertAndFormatMeasurement, formatValue } from "@/protoOS/store";
import { useIntervalMs, useTemperatureUnit } from "@/protoOS/store";
import Popover from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants.ts";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { positions } from "@/shared/constants";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { formatHashrateWithUnit } from "@/shared/utils/utility";

// import { dangerTemp } from "../../constants";
import { getRowLabel } from "@/shared/utils/utility";

interface AsicPopoverProps {
  asic: AsicData;
  closePopover: () => void;
  closeIgnoreSelectors?: string[];
}

const AsicPopover = ({ asic, closePopover, closeIgnoreSelectors }: AsicPopoverProps) => {
  const temperatureUnit = useTemperatureUnit();
  const intervalMs = useIntervalMs();

  // Convert telemetry data to chart format
  const { temperatureData, hashrateData } = useMemo(() => {
    const temperatureData = asic.temperature?.timeSeries
      ? convertTelemetryTemperatureToChartData(asic.temperature.timeSeries, intervalMs)
      : [];

    const hashrateData = asic.hashrate?.timeSeries
      ? convertTelemetryHashrateToChartData(asic.hashrate.timeSeries, intervalMs)
      : [];

    return { temperatureData, hashrateData };
  }, [asic.temperature, asic.hashrate, intervalMs]);

  const hashRateValue = hashrateData.length ? hashrateData[hashrateData.length - 1].value : 0;

  // TODO: [STORE_REFACTOR] we can probably use getCurrentValue from the store instead of formatHashrateWithUnit
  // Deprioritize for now, since this feature is turned off
  const { value: hashRate, unit: hashUnit } = formatHashrateWithUnit(hashRateValue ? hashRateValue : undefined);

  // Check if we're still loading data (no telemetry data available)
  const isLoading = !asic.temperature?.timeSeries && !asic.hashrate?.timeSeries;

  return (
    <Popover
      className="h-fit pb-3"
      position={positions.top}
      offset={minimalMargin * 3}
      closePopover={closePopover}
      closeIgnoreSelectors={closeIgnoreSelectors}
    >
      <div className="space-y-1">
        <div className="text-200 text-text-primary-70">ASIC</div>
        <div className="text-heading-200 text-text-primary">
          {getRowLabel(asic.row || 0)}
          {(asic.column || 0) + 1}
        </div>
        {/* TODO: update this condition when we have set indicators */}
        {/* {(asic.temp_c || 0) >= dangerTemp && (
          <div className="text-200 text-intent-warning-text text-wrap">
            Based on historical behavior, it’s likely this ASIC will cause the
            board to overheat.
          </div>
        )} */}
      </div>
      <div className="h-[92px] w-[272px]">
        {isLoading ? (
          <div className="flex h-full items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        ) : null}
        {hashrateData.length || temperatureData.length ? (
          <AsicChart hashrateData={hashrateData} temperatureData={temperatureData} />
        ) : !isLoading ? (
          <div className="flex h-full items-center justify-center text-text-primary-50">No chart data available</div>
        ) : null}
      </div>
      <div>
        <AsicPopoverRow
          label="Current temperature"
          value={
            asic.temperature?.latest
              ? convertAndFormatMeasurement(asic.temperature.latest, temperatureUnit, false)
              : undefined
          }
          className="text-core-accent-fill"
        />
        <AsicPopoverRow
          label="Current hashrate"
          value={hashrateData.length ? `${getDisplayValue(hashRate)} ${hashUnit}` : undefined}
          className="text-text-primary"
        />
        <AsicPopoverRow
          label="Voltage"
          value={asic.voltage?.latest ? formatValue(asic.voltage.latest, true) : undefined}
          className="text-text-primary"
        />
        <AsicPopoverRow
          label="Frequency"
          value={asic.frequency?.latest ? formatValue(asic.frequency.latest, true) : undefined}
          className="text-text-primary"
        />
      </div>
    </Popover>
  );
};

export default AsicPopover;
