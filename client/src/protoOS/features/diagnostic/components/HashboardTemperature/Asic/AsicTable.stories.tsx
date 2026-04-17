import { ElementType, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { AsicMetricProvider, type SelectedMetric } from "../AsicMetricContext";
import AsicTableComponent from "./AsicTable";
import { mockHashboardStats } from "./constants";
import { useTemperatureUnit } from "@/protoOS/store";
import DurationSelector, { type Duration, durations } from "@/shared/components/DurationSelector";
import SegmentedControl from "@/shared/components/SegmentedControl";

interface AsicTableProps {
  pending: boolean;
}

export const AsicTable = ({ pending }: AsicTableProps) => {
  const [duration, setDuration] = useState<Duration>(durations[0]);
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const [selectedMetric, setSelectedMetric] = useState<SelectedMetric>("temperature");
  const temperatureUnit = useTemperatureUnit();

  return (
    <div>
      <div className="mb-4 flex justify-end">
        <DurationSelector className="h-fit" duration={duration} onSelect={setDuration} />
      </div>
      <div className="mb-6">
        <SegmentedControl
          segments={[
            {
              key: "temperature",
              title: `Temperature (°${temperatureUnit})`,
            },
            {
              key: "hashrate",
              title: "Hashrate (GH/s)",
            },
            {
              key: "frequency",
              title: "Frequency (MHz)",
            },
            {
              key: "voltage",
              title: "Voltage (V)",
            },
          ]}
          onSelect={(metric) => setSelectedMetric(metric as SelectedMetric)}
        />
      </div>
      <AsicMetricProvider selectedMetric={selectedMetric}>
        <AsicTableComponent
          asics={pending ? [] : mockHashboardStats.asics}
          hashboardSerialNumber={mockHashboardStats.hb_sn}
          pending={pending}
          showPopover={showPopover}
          setShowPopover={setShowPopover}
        />
      </AsicMetricProvider>
    </div>
  );
};

export default {
  title: "ProtoOS/Asic Table",
  parameters: {
    withRouter: false,
  },
  args: {
    pending: false,
  },
  argTypes: {
    pending: {
      control: {
        type: "boolean",
      },
    },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
