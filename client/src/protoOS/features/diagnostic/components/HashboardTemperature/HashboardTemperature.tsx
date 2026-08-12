import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import AsicTable from "./Asic/AsicTableWrapper";
import { AsicMetricProvider, type SelectedMetric } from "./AsicMetricContext";
import HashboardSelector from "./HashboardSelector";
import { useTelemetry } from "@/protoOS/api";
import {
  type Rail,
  type RailEnd,
  type RailReading,
  useHashboardLayout,
} from "@/protoOS/features/diagnostic/hashboardLayout";
import {
  convertAndFormatMeasurement,
  convertValueUnits,
  formatValue,
  HashboardData,
  type Measurement,
  type TemperatureUnit,
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

const pagePadX = "px-6 tablet:px-10 laptop:px-14";
const pageMarginX = "mx-6 tablet:mx-10 laptop:mx-14";

/** A horizontal rule with label chips sitting on top of it. */
const railChrome =
  "relative flex items-center font-mono text-mono-text-50 text-text-primary-50 before:absolute before:top-[50%] before:left-0 before:h-[1px] before:w-full before:bg-border-5";
const railChip = "relative bg-surface-base";

/** Latest reading for each rail temperature, keyed the way rails refer to them. */
type RailReadings = Record<RailReading, Measurement | undefined>;

type HashboardRailProps = {
  rail: Rail;
  /** Optional centered label (the top rail carries the board serial). */
  center?: string;
  readings: RailReadings;
  temperatureUnit: TemperatureUnit;
};

/**
 * A labelled rail running alongside the ASIC grid. Which edge names appear and
 * which temperature each end shows come from the device's layout descriptor.
 */
const HashboardRail = ({ rail, center, readings, temperatureUnit }: HashboardRailProps) => {
  const endText = (end: RailEnd) => {
    const measurement = readings[end.reading];

    return measurement ? `${end.label} ${convertAndFormatMeasurement(measurement, temperatureUnit, false)}` : end.label;
  };

  return (
    <div className={`${railChrome} justify-between`}>
      <div className={`${railChip} pr-4`}>{endText(rail.left)}</div>
      {center ? <div className={`${railChip} px-4`}>{center}</div> : null}
      <div className={`${railChip} pl-4`}>{endText(rail.right)}</div>
    </div>
  );
};

type HashboardTemperatureProps = {
  serial: string;
};

const HashboardTemperature = ({ serial }: HashboardTemperatureProps) => {
  const temperatureUnit = useTemperatureUnit();
  const layout = useHashboardLayout();
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

  // Rail temperatures. Where each reading surfaces depends on airflow direction,
  // which the layout descriptor encodes: rigs run front-to-rear (one rail, inlet
  // and outlet at opposite ends), container modules run bottom-to-top (outlet
  // across the top rail, inlet across the bottom).
  const railReadings: RailReadings = {
    inlet: hashboard?.inletTemp?.latest,
    outlet: hashboard?.outletTemp?.latest,
  };

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
        titleSize={layout.headerTitleSize}
        buttons={[
          {
            text: "Done",
            variant: "primary",
            onClick: close,
          },
        ]}
      />
      <div className={`pt-24 pb-8 ${pagePadX}`}>
        <PopoverProvider>
          <HashboardSelector hashboardList={hashboardList} currentHashboard={serial} />
        </PopoverProvider>
      </div>
      <div className="max-w-screen overflow-visible overflow-x-auto">
        <div className={`${pagePadX} phone:mx-6 phone:!px-0`}>
          <Stats stats={stats} size="medium" gap="gap-10" padding="pb-4" />
        </div>
      </div>
      <div className={`my-6 ${pagePadX}`}>
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
      <div className={`${pagePadX} pt-4`}>
        {serial ? (
          <HashboardRail
            rail={layout.rails.top}
            center={serial}
            readings={railReadings}
            temperatureUnit={temperatureUnit}
          />
        ) : null}
      </div>
      {serial ? (
        <div className="scrollbar-hide max-w-screen overflow-x-auto">
          <div className={`relative ${pageMarginX} mb-2 ${layout.grid.minWidth}`}>
            <AsicMetricProvider selectedMetric={selectedMetric}>
              <AsicTable hashboardSerialNumber={serial} showPopover={showPopover} setShowPopover={setShowPopover} />
            </AsicMetricProvider>
          </div>
        </div>
      ) : null}
      {serial && layout.rails.bottom ? (
        <div className={`${pagePadX} pt-4`}>
          <HashboardRail rail={layout.rails.bottom} readings={railReadings} temperatureUnit={temperatureUnit} />
        </div>
      ) : null}
      <div className={`${pagePadX} mb-5`}>
        {hashboard?.board ? (
          <div className={`${railChrome} justify-around`}>
            <div className={`${railChip} px-4`}>{hashboard.board}</div>
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default HashboardTemperature;
