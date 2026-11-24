import clsx from "clsx";
import type { SelectedMetric } from "../../AsicMetricContext";
import { formatMetric, getMetricValue } from "../metricDisplay";
import AsicPopoverRow from "./AsicPopoverRow";
import { useHashboardLayout } from "@/protoOS/features/diagnostic/hashboardLayout";
import { type AsicData, useTemperatureUnit } from "@/protoOS/store";
import Popover from "@/shared/components/Popover";
import { minimalMargin } from "@/shared/components/Popover/constants";
import { positions } from "@/shared/constants";

interface AsicPopoverProps {
  asic: AsicData;
  selectedMetric?: SelectedMetric;
  closePopover: () => void;
  closeIgnoreSelectors?: string[];
}

const rows: { metric: SelectedMetric; label: string }[] = [
  { metric: "temperature", label: "Temperature" },
  { metric: "hashrate", label: "Hashrate" },
  { metric: "voltage", label: "Voltage" },
  { metric: "frequency", label: "Frequency" },
];

const AsicPopover = ({
  asic,
  selectedMetric = "temperature",
  closePopover,
  closeIgnoreSelectors,
}: AsicPopoverProps) => {
  const temperatureUnit = useTemperatureUnit();
  const layout = useHashboardLayout();
  return (
    <Popover
      className="h-fit pb-3"
      testId="asic-popover"
      position={positions.top}
      offset={minimalMargin * 3}
      constrainHeightToViewport
      closePopover={closePopover}
      closeIgnoreSelectors={closeIgnoreSelectors}
    >
      <div id="asic-live-readings" role="region" aria-label={`ASIC ${layout.labelDetail(asic)} live telemetry`}>
        <div className="mb-4 space-y-1">
          <div className="text-200 text-text-primary-70">ASIC</div>
          <div className="text-heading-200 text-text-primary">{layout.labelDetail(asic)}</div>
        </div>
        {rows.map(({ metric, label }) => (
          <div
            key={metric}
            className={clsx("-mx-2 rounded-lg px-2", metric === selectedMetric && "bg-surface-overlay")}
          >
            <AsicPopoverRow label={label} value={formatMetric(metric, getMetricValue(asic, metric), temperatureUnit)} />
          </div>
        ))}
      </div>
    </Popover>
  );
};

export default AsicPopover;
