import { AsicStats } from "apiTypes";

import { positions } from "common/constants";

import Popover from "components/Popover";

import { dangerTemp } from "../../constants";
import { getRowLabel } from "../../utility";
import AsicChart from "./AsicChart";
import AsicPopoverRow from "./AsicPopoverRow";

interface AsicPopoverProps {
  asic: AsicStats;
}

const AsicPopover = ({ asic }: AsicPopoverProps) => {
  return (
    <Popover position={positions["top right"]} className="mb-[58px] -left-[115px] pb-3 phone:left-0 phone:top-0 phone:mb-0 h-fit">
      <div className="space-y-1">
        <div className="text-200 text-text-primary/70">ASIC</div>
        <div className="text-heading-200 text-text-primary/90">
          {getRowLabel(asic.row || 0)}
          {(asic.column || 0) + 1}
        </div>
        {/* TODO: update this condition when we have set indicators */}
        {(asic.temp_c || 0) >= dangerTemp && (
          <div className="text-200 text-intent-warning-text text-wrap">
            Based on historical behavior, it’s likely this ASIC will cause the
            board to overheat.
          </div>
        )}
      </div>
      <div className="w-[272px] h-[92px]">
        <AsicChart />
      </div>
      <div>
        <AsicPopoverRow
          label="Temperature"
          value={`${asic.temp_c}º`}
          className="text-core-accent-fill"
        />
        <AsicPopoverRow
          label="Hashrate"
          value={`${asic.hashrate_ghs} TH/s`}
          className="text-text-primary"
        />
      </div>
    </Popover>
  );
};

export default AsicPopover;
