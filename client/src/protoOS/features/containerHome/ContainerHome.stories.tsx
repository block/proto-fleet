import { useState } from "react";
import ContainerHomeToolbar from "./components/ContainerHomeToolbar";
import ContainerMetricView from "./components/ContainerMetricView";
import ContainerOverviewView from "./components/ContainerOverviewView";
import type { HomeChartMode, HomeHistory, HomeReading, HomeSeries, ModuleGrid, OverviewModuleStatus } from "./types";
import type { Duration } from "@/shared/components/DurationSelector";

const end = Date.parse("2026-09-15T12:00:00Z");
const colors = ["#ff7626", "#783bf0", "#258fbf", "var(--color-core-primary-fill)", "#ff3344"];
const series: HomeSeries[] = [
  { label: "Hashboard avg", color: colors[0] },
  { label: "Fluid inlet", color: colors[2] },
  { label: "Fluid outlet", color: colors[4] },
  { label: "Ambient outdoor", color: colors[3] },
  { label: "Ambient indoor", color: colors[1] },
].map(({ label, color }, index) => ({
  id: label.toLowerCase(),
  label,
  color,
  latest: `${12 + index}.0 kW`,
  points: Array.from({ length: 145 }, (_, point) => ({
    datetime: end - (144 - point) * 300_000,
    value: 12 + index + Math.sin(point / 10),
  })),
  high: "16.0 kW",
  low: "10.0 kW",
  average: "12.0 kW",
}));
const powerSeries: HomeSeries[] = [
  { label: "Facility total", value: 820, color: colors[3] },
  { label: "Mining power", value: 720, color: colors[0] },
  ...Array.from({ length: 6 }, (_, index) => ({
    label: `pdu-${index + 1}`,
    value: 125 + index,
    color: `hsl(${(270 + index * 137.508) % 360} 65% 50%)`,
  })),
  { label: "Fans", value: 20, color: colors[2] },
].map(({ label, value, color }) => ({
  id: label,
  label,
  color,
  latest: `${value}.0 kW`,
  points: series[0].points.map((point, index) => ({ ...point, value: value + Math.sin(index / 10) })),
  high: `${value + 1}.0 kW`,
  low: `${value - 1}.0 kW`,
  average: `${value}.0 kW`,
}));
const history: HomeHistory = { series: powerSeries, units: "kW", window: [end - 12 * 3_600_000, end] };
const readings: HomeReading[] = [
  "Container power",
  "Container efficiency",
  "Module high",
  "Module low",
  "Module average",
  "Module efficiency",
].map((label, index) => ({ id: `${index}`, label, value: label.includes("efficiency") ? "15.5 J/TH" : "11.5 kW" }));
const links = {
  overview: "/",
  hashrate: "/hashrate",
  efficiency: "/efficiency",
  power: "/power-usage",
  temperature: "/temperature",
  system: "/?view=system",
};
const locations = ["L1", "L2", "L3", "R1", "R2", "R3"].map((label, index) => ({
  id: label,
  label,
  amount: index === 4 ? null : 12 + index,
  value: index === 4 ? "—" : `${12 + index} kW`,
}));

interface StoryDataOptions {
  empty: boolean;
  zero: boolean;
  temperature: boolean;
}

const temperatureReadings: HomeReading[] = [
  "Ambient inside",
  "Ambient outside",
  "Fluid",
  "Hashboard high",
  "Hashboard low",
  "Hashboard avg",
].map((label) => ({ id: label, label, value: "65.5 °C" }));
const temperatureLocations = locations.map((item) => ({
  ...item,
  value: item.amount === null ? "—" : "65.5 °C",
}));

function buildStoryReadings({ empty, zero, temperature }: StoryDataOptions): HomeReading[] {
  const source = temperature ? temperatureReadings : readings;
  if (empty) return source.map((reading) => ({ ...reading, value: "—" }));
  if (zero)
    return source.map((reading) => ({
      ...reading,
      value: reading.label.includes("efficiency") ? "—" : "0.0 kW",
    }));
  return source;
}

function buildStorySeries({
  empty,
  zero,
  temperature,
  partial,
}: StoryDataOptions & { partial: boolean }): HomeSeries[] {
  if (empty) return [];
  let source = temperature ? series : powerSeries;
  if (partial)
    source = source.map((item) =>
      item.label === "Fans"
        ? { ...item, latest: "—", points: item.points.map((point) => ({ ...point, value: null })) }
        : item,
    );

  if (zero)
    return source.map((item) => ({
      ...item,
      latest: "0.0 kW",
      points: item.points.map((point) => ({ ...point, value: 0 })),
    }));
  if (temperature) return source.map((item) => ({ ...item, latest: "65.5 °C" }));
  return source;
}

function MetricStory({
  empty = false,
  temperature = false,
  partial = false,
  loading = false,
  error = false,
  zero = false,
  initialMode = "system",
}: {
  empty?: boolean;
  temperature?: boolean;
  partial?: boolean;
  loading?: boolean;
  error?: boolean;
  zero?: boolean;
  initialMode?: HomeChartMode;
}) {
  const [mode, setMode] = useState<HomeChartMode>(initialMode);
  const [duration, setDuration] = useState<Duration>("12h");
  const modes = [
    { id: "system" as const, label: "System" },
    ...(temperature ? [{ id: "fluid" as const, label: "Fluid" }] : []),
  ];
  const dataOptions = { empty, zero, temperature };
  return (
    <div className="min-h-screen bg-surface-base p-10 text-text-primary">
      <div className="mb-6">
        <ContainerHomeToolbar
          tab={temperature ? "temperature" : "power"}
          links={links}
          duration={duration}
          onDurationChange={setDuration}
        />
      </div>
      <ContainerMetricView
        title={temperature ? "Temperature" : "Power"}
        readings={buildStoryReadings(dataOptions)}
        mode={mode}
        modes={temperature ? modes : []}
        onModeChange={setMode}
        locations={temperature ? temperatureLocations : locations}
        history={{
          ...history,
          series: buildStorySeries({ ...dataOptions, partial }),
          units: temperature ? "°C" : "kW",
          stacked: false,
          loading,
          error: error ? "Could not refresh history. Displayed history may be out of date." : undefined,
        }}
      />
    </div>
  );
}

export const Power = () => <MetricStory />;
export const PartialTelemetry = () => <MetricStory partial />;
export const UnavailableTelemetry = () => <MetricStory empty />;
export const TemperatureModes = () => <MetricStory temperature />;
export const TemperatureFluid = () => <MetricStory temperature initialMode="fluid" />;
export const Loading = () => <MetricStory empty loading />;
export const StaleHistory = () => <MetricStory error />;
export const ZeroPower = () => <MetricStory zero />;

const overviewChart = (id: string, label: string, units: string, value: number) => {
  const source: HomeSeries = {
    id,
    label,
    color: "var(--color-core-accent-fill)",
    latest: `${value.toFixed(1)} ${units}`,
    high: `${(value + 1).toFixed(1)} ${units}`,
    low: `${(value - 1).toFixed(1)} ${units}`,
    points: series[0].points.map((point, index) => ({ ...point, value: value + Math.sin(index / 10) })),
  };
  return {
    id,
    label,
    stats: { label, value: source.latest, preformatted: true, valueSuffix: `Max ${source.high} · Min ${source.low}` },
    history: { ...history, stacked: false, units, series: [source] },
  };
};
const moduleStates = (tank: number, module: number): OverviewModuleStatus =>
  tank === 1 && module % 2 === 0 ? "sleeping" : tank === 2 && module === 3 ? "attention" : "healthy";
const overviewGrid: ModuleGrid = {
  tanks: [1, 2, 3, 4, 5, 6].map((tank) => ({
    id: `tank-${tank}`,
    label: `Tank ${tank}`,
    slot: tank,
    columns: 8,
    rows: 2,
    modules: Array.from({ length: 16 }, (_, index) => {
      const state = moduleStates(tank, index + 1);
      return {
        id: `${tank}-${index}`,
        label: String(index + 1).padStart(2, "0"),
        position: { column: Math.floor(index / 2) + 1, row: 2 - (index % 2) },
        name: `Tank ${tank} module ${index + 1}`,
        to: "/tanks",
        state,
        boards: state === "attention" ? ["healthy", "attention", "healthy"] : [state, state, state],
      };
    }),
  })),
  counts: { attention: 1, offline: 0, sleeping: 8, healthy: 87, unknown: 0 },
};
function OverviewStory({ quiet = false }: { quiet?: boolean }) {
  const [duration, setDuration] = useState<Duration>("12h");
  return (
    <div className="bg-surface-base p-10 text-text-primary">
      <ContainerOverviewView
        duration={duration}
        onDurationChange={setDuration}
        production={[
          overviewChart("hashrate", "Hashrate", "PH/s", 48),
          { ...overviewChart("temperature", "Average hashboard temp", "°C", 49), label: "Temperature" },
          overviewChart("power", "Power", "kW", 820),
          overviewChart("efficiency", "Efficiency", "J/TH", 17.1),
        ]}
        grid={overviewGrid}
        issues={
          quiet
            ? { critical: 0, warning: 0, groups: [] }
            : {
                critical: 2,
                warning: 2,
                groups: [
                  { id: "ac1", label: "Air conditioner #1 fault", severity: "critical", affected: "1 container" },
                  { id: "psu", label: "Power supply over-limit alarm", severity: "critical", affected: "1 container" },
                  { id: "wiring", label: "Wiring stale", severity: "warning", affected: "154 boards" },
                  { id: "storage", label: "Blackbox storage fault", severity: "warning", affected: "3 boards" },
                ],
              }
        }
        onViewIssues={() => {}}
      />
    </div>
  );
}
export const Overview = () => <OverviewStory />;
export const OverviewNoIssues = () => <OverviewStory quiet />;

export default { title: "Proto OS/Containers/Home", component: ContainerMetricView };
