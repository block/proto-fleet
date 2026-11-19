import { useState } from "react";
import ContainerHomeToolbar from "./components/ContainerHomeToolbar";
import ContainerMetricView from "./components/ContainerMetricView";
import ContainerOverviewView from "./components/ContainerOverviewView";
import type { HomeChartMode, HomeHistory, HomeReading, HomeSeries, ModuleGrid } from "./types";
import type { ModuleStatus } from "@/protoOS/features/tanks/tankView";
import type { Duration } from "@/shared/components/DurationSelector";

const end = Date.parse("2026-09-15T12:00:00Z");
const colors = ["#ff7626", "#783bf0", "#258fbf", "var(--color-core-primary-fill)", "#ff3344"];
const series: HomeSeries[] = ["Hashboards", "Pumps", "Fans", "AC", "Aux"].map((label, index) => ({
  id: label.toLowerCase(),
  label,
  color: colors[index],
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
  "Ambient temp",
  "Outdoor temp",
  "Fluid temp",
  "Module high",
  "Module low",
  "Module average",
].map((label) => ({ id: label, label, value: "65.5 °C" }));
const temperatureLocations = locations.map((item) => ({
  ...item,
  value: item.amount === null ? "—" : "65.5 °C",
}));

const bandSeries = Array.from({ length: 10 }, (_, index) => ({
  ...series[0],
  id: `band-${index}`,
  label: `${91 - index * 10}–${100 - index * 10}%`,
  color: colors[index % colors.length],
  points: series[0].points.map((point) => ({
    ...point,
    value: point.value === null ? null : point.value + index * 3,
  })),
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
  mode,
  partial,
}: StoryDataOptions & { mode: HomeChartMode; partial: boolean }): HomeSeries[] {
  if (empty) return [];
  let source = temperature ? series : powerSeries;
  if (temperature && mode === "hashboards") source = bandSeries;
  else if (partial)
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

function storyStatistics(item: HomeSeries, { empty, zero, temperature }: StoryDataOptions) {
  if (empty) return { high: "—", low: "—", average: "—" };
  if (zero) return { high: "0.0 kW", low: "0.0 kW", average: "0.0 kW" };
  if (temperature) return { high: "68.0 °C", low: "60.0 °C", average: "65.5 °C" };
  return { high: item.high!, low: item.low!, average: item.average! };
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
    { id: "hashboards" as const, label: "Hashboards" },
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
          series: buildStorySeries({ ...dataOptions, mode, partial }),
          units: temperature ? "°C" : "kW",
          stacked: false,
          loading,
          error: error ? "Could not refresh history. Displayed history may be out of date." : undefined,
        }}
        categories={(temperature ? series.slice(1) : powerSeries.filter((item) => item.label === "Fans")).map(
          (item) => ({
            id: item.id,
            label: item.label,
            ...storyStatistics(item, { ...dataOptions, empty: empty || partial }),
          }),
        )}
      />
    </div>
  );
}

export const Power = () => <MetricStory />;
export const PartialTelemetry = () => <MetricStory partial />;
export const UnavailableTelemetry = () => <MetricStory empty />;
export const TemperatureModes = () => <MetricStory temperature />;
// Illustrative band data only: live band membership/statistics require a definition.
export const TemperatureHashboards = () => <MetricStory temperature initialMode="hashboards" />;
export const TemperatureFluid = () => <MetricStory temperature initialMode="fluid" />;
export const Loading = () => <MetricStory empty loading />;
export const StaleHistory = () => <MetricStory error />;
export const ZeroPower = () => <MetricStory zero />;

const overviewChart = (id: string, label: string, units: string, source: HomeSeries) => ({
  id,
  label,
  to: `/${id}`,
  low: source.low ?? "—",
  high: source.high ?? "—",
  history: { ...history, stacked: false, units, series: [{ ...source, id, label }] },
});
const moduleStates = (tank: number, module: number): ModuleStatus =>
  tank === 1 && module % 2 === 0 ? "sleeping" : tank === 2 && module === 3 ? "attention" : "healthy";
const overviewGrid: ModuleGrid = {
  columns: 16,
  tanks: [1, 2, 3, 4].map((tank) => ({
    id: `tank-${tank}`,
    label: `Tank ${tank}`,
    modules: Array.from({ length: 16 }, (_, index) => {
      const state = moduleStates(tank, index + 1);
      return {
        id: `${tank}-${index}`,
        column: index + 1,
        name: `Tank ${tank} module ${index + 1}`,
        to: "/tanks",
        state,
        boards: state === "attention" ? ["healthy", "attention", "healthy"] : [state, state, state],
      };
    }),
  })),
  counts: { attention: 1, offline: 0, isolated: 0, sleeping: 8, healthy: 55, unknown: 0 },
};
function OverviewStory({ quiet = false }: { quiet?: boolean }) {
  const [duration, setDuration] = useState<Duration>("12h");
  return (
    <div className="bg-surface-base p-10 text-text-primary">
      <ContainerOverviewView
        duration={duration}
        onDurationChange={setDuration}
        kpis={[
          { label: "Hashrate", value: "28,050.0 TH/s", text: "97.4% of 28,800.0 TH/s ideal", chartPercentage: 97.4 },
          { label: "Power", value: "437.8 kW" },
          { label: "Efficiency", value: "17.0 J/TH", text: "Hashboards 15.6 J/TH" },
          { label: "Hashboards active", value: "157 / 158", text: "1 inactive", chartPercentage: 99.4 },
        ].map((stat) => ({ ...stat, preformatted: true }))}
        production={[
          overviewChart("hashrate", "Hashrate", "TH/s", series[0]),
          overviewChart("power", "Power", "kW", series[2]),
          overviewChart("efficiency", "Efficiency", "J/TH", series[1]),
          {
            id: "temperature",
            label: "Temperature",
            to: "/temperature",
            low: "41.2 °C",
            high: "48.6 °C",
            history: {
              ...history,
              stacked: false,
              units: "°C",
              series: [
                { ...series[3], id: "left", label: "Left tanks" },
                { ...series[4], id: "right", label: "Right tanks" },
              ],
            },
          },
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
        links={{ tanks: "/tanks" }}
        onViewIssues={() => {}}
      />
    </div>
  );
}
export const Overview = () => <OverviewStory />;
export const OverviewNoIssues = () => <OverviewStory quiet />;

export default { title: "Proto OS/Containers/Home", component: ContainerMetricView };
