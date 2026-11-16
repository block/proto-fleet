import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";
import AsicTable from "./Asic/AsicTableWrapper";
import { AsicMetricProvider, type SelectedMetric } from "./AsicMetricContext";
import HashboardSelector from "./HashboardSelector";
import { useTelemetry } from "@/protoOS/api";
import LocationBreadcrumb from "@/protoOS/components/LocationBreadcrumb/LocationBreadcrumb";
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
  temperatureDisplayUnit,
  type TemperatureUnit,
  useHashboardsHardware,
  useMinerHashboard,
  useMinerStore,
  useTemperatureUnit,
} from "@/protoOS/store";
import { efficiencyValue } from "@/protoOS/store/utils/efficiency";
import { Alert, ArrowRight, Dismiss } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import { PopoverProvider } from "@/shared/components/Popover";
import SegmentedControl from "@/shared/components/SegmentedControl";
import Stats from "@/shared/components/Stats";

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

    return measurement
      ? `${end.label} ${convertAndFormatMeasurement(measurement, temperatureDisplayUnit(temperatureUnit), false)}`
      : end.label;
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
  /** The last refresh failed, so the readings shown are not current. */
  readingsStale?: boolean;
};

const HashboardTemperature = ({ serial, readingsStale = false }: HashboardTemperatureProps) => {
  const temperatureUnit = useTemperatureUnit();
  const layout = useHashboardLayout();
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const [selectedMetric, setSelectedMetric] = useState<SelectedMetric>("temperature");

  const navigate = useNavigate();
  const { search } = useLocation();

  // Fetch latest telemetry data with polling
  // TODO: [STORE_REFACTOR] Telemetry API will give include miner and hashboard level data when we specify level=asic
  // We have another polling call in parent component KpiLayout.  If we want to remove extra requests we could add some logic to useTelemetry
  // so that the keeps track of the polling requests somehow and only lets the most specific one (level=asic) poll
  useTelemetry({
    level: ["asic"],
  });

  const close = () => {
    navigate({ pathname: "..", search }, { relative: "path" });
  };

  // Get hashboard data from store
  const hashboard = useMinerHashboard(serial);

  // Subscribe only to hardware slice to avoid telemetry updates triggering hashboardList recomputation
  const hardwareHashboards = useHashboardsHardware();
  const topology = useMinerStore((state) => state.hardware.topology);
  const currentBoard = useMemo(
    () => [...(topology?.hashboards.values() ?? [])].find((board) => board.identity.value === serial),
    [topology, serial],
  );

  // Memoize hashboard list - only recreate when hardware changes, not on telemetry updates
  const hashboardList = useMemo(() => {
    if (topology?.kind === "container") {
      return [...topology.hashboards.values()]
        .filter((board) => currentBoard && board.moduleId === currentBoard.moduleId)
        .sort((a, b) => (a.location?.slot ?? Infinity) - (b.location?.slot ?? Infinity))
        .map((board) => ({ serial: board.identity.value, name: layout.card.title(board.location?.slot) }));
    }
    return hardwareHashboards
      .filter((h): h is HashboardData & { slot: number } => !!h.slot)
      .sort((a, b) => a.slot - b.slot)
      .map((board) => ({ serial: board.serial, name: `Hashboard ${board.slot}` }));
  }, [hardwareHashboards, topology, currentBoard, layout]);

  const stats = useMemo(() => {
    const efficiency = efficiencyValue(
      convertValueUnits(hashboard?.power?.latest, "W")?.value ?? undefined,
      convertValueUnits(hashboard?.hashrate?.latest, "TH/s")?.value ?? undefined,
      hashboard?.efficiency?.latest?.value,
    );
    const measurements = {
      averageAsicTemperature: convertValueUnits(
        hashboard?.averageAsicTemperature?.latest,
        temperatureDisplayUnit(temperatureUnit),
      ),
      maximumAsicTemperature: convertValueUnits(
        hashboard?.maximumAsicTemperature?.latest,
        temperatureDisplayUnit(temperatureUnit),
      ),
      power: convertValueUnits(hashboard?.power?.latest, "kW"),
      hashrate: convertValueUnits(hashboard?.hashrate?.latest, "TH/s"),
      efficiency: efficiency === null ? undefined : { value: efficiency, units: "J/TH" as const },
    };
    return layout.detailStats.map(({ metric, label }) => ({
      label,
      value: metric === "efficiency" && efficiency === null ? "—" : formatValue(measurements[metric]),
      units: measurements[metric]?.units,
    }));
  }, [hashboard, layout, temperatureUnit]);

  // Rail temperatures. Where each reading surfaces depends on airflow direction,
  // which the layout descriptor encodes: rigs run front-to-rear (one rail, inlet
  // and outlet at opposite ends), container modules run bottom-to-top (outlet
  // across the top rail, inlet across the bottom).
  const railReadings: RailReadings = {
    inlet: hashboard?.inletTemperature?.latest,
    outlet: hashboard?.outletTemperature?.latest,
  };

  return (
    <div className="min-h-[100vh] w-full bg-surface-base">
      <Header
        className="fixed top-0 z-10 h-16 items-center border-b border-border-5 bg-surface-base px-4"
        centerButton={true}
        icon={
          layout.headerIcon === "back" ? <ArrowRight className="rotate-180" width="w-5" /> : <Dismiss width="w-3.5" />
        }
        iconAriaLabel={layout.headerIcon === "back" ? "Back to module" : "Close hashboards"}
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
      <div className={`flex items-center gap-2 pt-24 pb-8 ${pagePadX}`}>
        {topology?.kind === "container" && currentBoard ? (
          <LocationBreadcrumb topology={topology} moduleId={currentBoard.moduleId} board={currentBoard} />
        ) : null}
        <div className="shrink-0">
          <PopoverProvider>
            <HashboardSelector
              hashboardList={hashboardList}
              currentHashboard={serial}
              headingSize={topology?.kind === "container" ? "compact" : "base"}
            />
          </PopoverProvider>
        </div>
      </div>
      {readingsStale ? (
        <div role="alert" className={`pb-8 ${pagePadX}`}>
          <Callout
            intent="warning"
            prefixIcon={<Alert />}
            title="Could not refresh ASIC readings."
            subtitle="Showing the last readings received. Retrying automatically."
          />
        </div>
      ) : null}
      <div
        className={clsx("max-w-screen overflow-visible overflow-x-auto", readingsStale && "opacity-50")}
        data-testid="hashboard-stats"
      >
        <div className={`${pagePadX} phone:mx-6 phone:!px-0`}>
          <Stats stats={stats} grid={layout.detailStatsGrid} size="medium" gap="gap-10" padding="pb-4" />
        </div>
      </div>
      <div className={`my-6 ${pagePadX}`}>
        <SegmentedControl
          segments={[
            {
              key: "temperature",
              title: `Temperature (${temperatureDisplayUnit(temperatureUnit)})`,
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
      <div className={clsx(readingsStale && "opacity-50")} data-testid="hashboard-readings">
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
      </div>
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
