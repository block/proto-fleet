import AsicChart from "./AsicChart";
import { ChartData } from "./AsicChart/types";
import AsicPopoverRow from "./AsicPopoverRow";
import { convertAndFormatTemperature } from "./utility";
import { AsicStats } from "@/protoOS/api/types";
import Popover from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants.ts";
import Spinner from "@/shared/components/Spinner";
import { positions } from "@/shared/constants";
import { usePreferences } from "@/shared/features/preferences";
import { getDisplayValue } from "@/shared/utils/stringUtils";

// import { dangerTemp } from "../../constants";
import { getRowLabel } from "@/shared/utils/utility";

interface AsicPopoverProps {
  asic: AsicStats;
  hashrateData?: ChartData[];
  pendingAsicHashrateData?: boolean;
  pendingAsicTemperatureData?: boolean;
  temperatureData?: ChartData[];
  closePopover: () => void;
}

const AsicPopover = ({
  asic,
  hashrateData,
  pendingAsicHashrateData,
  pendingAsicTemperatureData,
  temperatureData,
  closePopover,
}: AsicPopoverProps) => {
  const { temperatureUnits } = usePreferences();

  return (
    <Popover
      className="h-fit pb-3"
      position={positions.top}
      offset={minimalMargin * 3}
      closePopover={closePopover}
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
        {(pendingAsicHashrateData && !hashrateData?.length) ||
        (pendingAsicTemperatureData && !temperatureData?.length) ? (
          <div className="flex h-full items-center justify-center">
            <Spinner />
          </div>
        ) : null}
        {hashrateData?.length && temperatureData?.length ? (
          <AsicChart
            hashrateData={hashrateData}
            temperatureData={temperatureData}
          />
        ) : null}
      </div>
      <div>
        <AsicPopoverRow
          label="Current temperature"
          value={
            temperatureData?.length &&
            `${convertAndFormatTemperature(temperatureData[temperatureData.length - 1].value, temperatureUnits)}º`
          }
          className="text-core-accent-fill"
        />
        <AsicPopoverRow
          label="Current hashrate"
          value={
            hashrateData?.length &&
            `${getDisplayValue(hashrateData[hashrateData.length - 1].value)} TH/s`
          }
          className="text-text-primary"
        />
      </div>
    </Popover>
  );
};

export default AsicPopover;
