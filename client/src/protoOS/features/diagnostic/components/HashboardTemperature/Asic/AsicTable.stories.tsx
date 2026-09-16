import { ElementType, useEffect, useMemo, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { AsicMetricProvider, type SelectedMetric } from "../AsicMetricContext";
import AsicTableComponent from "./AsicTable";
import { mockHashboardStats } from "./constants";
import { toCanonicalSystemInfo } from "@/protoOS/api/adapters/mdkV2/normalize";
import { type AsicData, useTemperatureUnit } from "@/protoOS/store";
import useMinerStore from "@/protoOS/store/useMinerStore";
import DurationSelector, { type Duration, durations } from "@/shared/components/DurationSelector";
import SegmentedControl from "@/shared/components/SegmentedControl";

interface AsicTableProps {
  ct1?: boolean;
  initialMetric?: SelectedMetric;
  pending: boolean;
}

export const AsicTable = ({ pending, ct1 = false, initialMetric = "temperature" }: AsicTableProps) => {
  const [duration, setDuration] = useState<Duration>(durations[0]);
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const [selectedMetric, setSelectedMetric] = useState<SelectedMetric>(initialMetric);
  const temperatureUnit = useTemperatureUnit();
  useEffect(() => {
    if (!ct1) return;
    useMinerStore
      .getState()
      .systemInfo.setSystemInfo(toCanonicalSystemInfo({ info: { id: "ct1-story", product: "CT1" } }));
    return () => useMinerStore.getState().resetDeviceData();
  }, [ct1]);
  const asics = useMemo<AsicData[]>(
    () =>
      ct1
        ? Array.from({ length: 312 }, (_, index) => ({
            id: `CT1-HB-1-asic-${index}`,
            hashboardSerial: "CT1-HB-1",
            index,
            row: Math.floor(index / 26),
            column: index % 26,
            temperature: index === 0 ? undefined : { latest: { value: 40 + (index % 50), units: "°C" } },
            hashrate: { latest: { value: index === 1 ? 0 : index === 2 ? 950 : 1042 + (index % 64), units: "GH/s" } },
            voltage: { latest: { value: 0.8 + (index % 10) / 100, units: "V" } },
            frequency: { latest: { value: 500 + (index % 20), units: "MHz" } },
          }))
        : mockHashboardStats.asics,
    [ct1],
  );

  return (
    <div>
      {!ct1 ? (
        <div className="mb-4 flex justify-end">
          <DurationSelector className="h-fit" duration={duration} onSelect={setDuration} />
        </div>
      ) : null}
      <div className="mb-6">
        <SegmentedControl
          initialSegmentKey={initialMetric}
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
              title: "Voltage (mV)",
            },
          ]}
          onSelect={(metric) => setSelectedMetric(metric as SelectedMetric)}
        />
      </div>
      <AsicMetricProvider selectedMetric={selectedMetric}>
        <AsicTableComponent
          asics={pending ? [] : asics}
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
  title: "Proto OS/Asic Table",
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

export const Ct1 = () => <AsicTable pending={false} ct1 />;
export const Ct1Hashrate = () => <AsicTable pending={false} ct1 initialMetric="hashrate" />;
export const Ct1Frequency = () => <AsicTable pending={false} ct1 initialMetric="frequency" />;
export const Ct1Voltage = () => <AsicTable pending={false} ct1 initialMetric="voltage" />;
