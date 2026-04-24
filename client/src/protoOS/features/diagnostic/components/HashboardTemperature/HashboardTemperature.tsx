import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import AsicTable from "./Asic/AsicTableWrapper";
import { AsicMetricProvider, type SelectedMetric } from "./AsicMetricContext";
import HashboardSelector from "./HashboardSelector";
import { useTelemetry } from "@/protoOS/api";
import {
  convertAndFormatMeasurement,
  convertValueUnits,
  formatValue,
  HashboardData,
  type Measurement,
  useHashboardsHardware,
  useMinerHashboard,
  useTemperatureUnit,
} from "@/protoOS/store";
import { Dismiss } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import { PopoverProvider } from "@/shared/components/Popover";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Stats, { type StatsProps } from "@/shared/components/Stats";

const getStats = (
  avgAsicTemp: Measurement | undefined,
  maxAsicTemp: Measurement | undefined,
  powerUsage: Measurement | undefined,
  hashrate: Measurement | undefined,
): StatsProps["stats"] => {
  return [
    {
      label: "Highest ASIC temp",
      value: formatValue(maxAsicTemp),
      units: maxAsicTemp?.units,
    },
    {
      label: "Avg ASIC temp",
      value: formatValue(avgAsicTemp),
      units: avgAsicTemp?.units,
    },
    {
      label: "Board power usage",
      value: formatValue(powerUsage),
      units: powerUsage?.units,
    },
    {
      label: "Board hashrate",
      value: formatValue(hashrate),
      units: hashrate?.units,
    },
  ];
};

const containerPadX = "px-14 tablet:px-10 phone:px-6";
const containerMarginX = "mx-14 tablet:mx-10 phone:mx-6";

type HashboardTemperatureProps = {
  serial: string;
};

const HashboardTemperature = ({ serial }: HashboardTemperatureProps) => {
  const temperatureUnit = useTemperatureUnit();
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const [selectedMetric, setSelectedMetric] = useState<SelectedMetric>("temperature");

  const navigate = useNavigate();

  // Fetch latest telemetry data with polling
  // TODO: [STORE_REFACTOR] Telemetry API will give include miner and hashboard level data when we specify level=asic
  // We have another polling call in parent component KpiLayout.  If we want to remove extra requests we could add some logic to useTelemetry
  // so that the keeps track of the polling requests somehow and only lets the most specific one (level=asic) poll
  useTelemetry({
    level: ["asic"],
  });

  const close = () => {
    navigate("..", { relative: "path" });
  };

  // Get hashboard data from store
  const hashboard = useMinerHashboard(serial);

  // Subscribe only to hardware slice to avoid telemetry updates triggering hashboardList recomputation
  const hardwareHashboards = useHashboardsHardware();

  // Memoize hashboard list - only recreate when hardware changes, not on telemetry updates
  const hashboardList = useMemo(() => {
    return hardwareHashboards
      .filter((h): h is HashboardData & { slot: number } => !!h.slot)
      .sort((a, b) => a.slot - b.slot)
      .map((hashboard) => ({
        serial: hashboard.serial,
        name: `Hashboard ${hashboard.slot}`,
      }));
  }, [hardwareHashboards]);

  // Memoize stats computation
  const stats = useMemo(
    () =>
      getStats(
        convertValueUnits(hashboard?.avgAsicTemp?.latest, temperatureUnit),
        convertValueUnits(hashboard?.maxAsicTemp?.latest, temperatureUnit),
        convertValueUnits(hashboard?.power?.latest, "kW"),
        convertValueUnits(hashboard?.hashrate?.latest, "TH/S"),
      ),
    [temperatureUnit, hashboard?.avgAsicTemp, hashboard?.maxAsicTemp, hashboard?.power, hashboard?.hashrate],
  );

  return (
    <div className="min-h-[100vh] w-full bg-surface-base">
      <Header
        className="fixed z-10 h-16 items-center border-b border-border-5 bg-surface-base px-4"
        centerButton={true}
        icon={<Dismiss width="w-3.5" />}
        iconAriaLabel="Close hashboards"
        iconVariant="textOnly"
        iconTextColor="text-text-primary"
        iconOnClick={close}
        inline={true}
        title="Hashboards"
        titleSize="text-heading-300"
        buttons={[
          {
            text: "Done",
            variant: "primary",
            onClick: close,
          },
        ]}
      />
      <div className={`pt-24 pb-8 ${containerPadX}`}>
        <PopoverProvider>
          <HashboardSelector hashboardList={hashboardList} currentHashboard={serial} />
        </PopoverProvider>
      </div>
      <div className="max-w-screen overflow-visible overflow-x-auto">
        <div className={`${containerPadX} phone:mx-6 phone:!px-0`}>
          <Stats stats={stats} size="medium" gap="gap-10" padding="pb-4" />
        </div>
      </div>
      <div className={`my-6 ${containerPadX}`}>
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
              key: "voltage",
              title: "Voltage (mV)",
            },
            {
              key: "frequency",
              title: "Frequency (MHz)",
            },
          ]}
          onSelect={(metric) => setSelectedMetric(metric as SelectedMetric)}
        />
      </div>
      <div className={`${containerPadX} pt-4`}>
        {serial ? (
          <div className="before:w-ful relative flex items-center justify-between font-mono text-mono-text-50 text-text-primary-50 before:absolute before:top-[50%] before:left-0 before:h-[1px] before:w-full before:bg-border-5">
            <div className="relative bg-surface-base pr-4">
              Front
              {hashboard?.inletTemp?.latest ? (
                <> {convertAndFormatMeasurement(hashboard.inletTemp.latest, temperatureUnit, false)}</>
              ) : null}
            </div>
            <div className="relative bg-surface-base px-4">{serial}</div>
            <div className="relative bg-surface-base pl-4">
              Rear
              {hashboard?.outletTemp?.latest ? (
                <> {convertAndFormatMeasurement(hashboard.outletTemp.latest, temperatureUnit, false)}</>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
      {serial ? (
        <div className="scrollbar-hide max-w-screen overflow-x-auto">
          <div className={`relative ${containerMarginX} mb-2 min-w-[800px]`}>
            <AsicMetricProvider selectedMetric={selectedMetric}>
              <AsicTable hashboardSerialNumber={serial} showPopover={showPopover} setShowPopover={setShowPopover} />
            </AsicMetricProvider>
          </div>
        </div>
      ) : null}
      <div className={`${containerPadX} mb-5`}>
        {hashboard?.board ? (
          <div className="before:w-ful relative flex items-center justify-around font-mono text-mono-text-50 text-text-primary-50 before:absolute before:top-[50%] before:left-0 before:h-[1px] before:w-full before:bg-border-5">
            <div className="relative bg-surface-base px-4">{hashboard.board}</div>
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default HashboardTemperature;
