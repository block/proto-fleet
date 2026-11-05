import { useState } from "react";
import ContainerHomeToolbar from "./components/ContainerHomeToolbar";
import ContainerMetricView from "./components/ContainerMetricView";
import type { HomeChartMode, HomeHistory, HomeReading, HomeSeries } from "./types";
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
const history: HomeHistory = { series, units: "kW", window: [end - 12 * 3_600_000, end], stacked: true };
const readings: HomeReading[] = [
  "Container power",
  "Container efficiency",
  "Module high",
  "Module low",
  "Module average",
  "Module efficiency",
  ...Array.from({ length: 6 }, (_, index) => `PDU ${index + 1}`),
].map((label, index) => ({ id: `${index}`, label, value: label.includes("efficiency") ? "15.5 J/TH" : "11.5 kW" }));
const links = {
  overview: "/",
  hashrate: "/hashrate",
  efficiency: "/efficiency",
  power: "/power-usage",
  temperature: "/temperature",
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
  let source = series;
  if (mode === "hashboards") source = bandSeries;
  else if (partial) source = series.slice(0, 2);

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
        modes={modes}
        onModeChange={setMode}
        locations={temperature ? temperatureLocations : locations}
        history={{
          ...history,
          series: buildStorySeries({ ...dataOptions, mode, partial }),
          units: temperature ? "°C" : "kW",
          stacked: !temperature && mode === "system" && !partial,
          loading,
          error: error ? "Could not refresh history. Displayed history may be out of date." : undefined,
        }}
        categories={series.slice(1).map((item) => ({
          id: item.id,
          label: item.label,
          ...storyStatistics(item, dataOptions),
        }))}
      />
    </div>
  );
}

export const Power = () => <MetricStory />;
export const PartialTelemetry = () => <MetricStory partial />;
export const UnavailableTelemetry = () => <MetricStory empty />;
export const TemperatureModes = () => <MetricStory temperature />;
// Illustrative band data only: live band membership/statistics require a definition.
export const PowerHashboards = () => <MetricStory initialMode="hashboards" />;
export const TemperatureHashboards = () => <MetricStory temperature initialMode="hashboards" />;
export const TemperatureFluid = () => <MetricStory temperature initialMode="fluid" />;
export const Loading = () => <MetricStory empty loading />;
export const StaleHistory = () => <MetricStory error />;
export const ZeroPower = () => <MetricStory zero />;

export default { title: "Proto OS/Containers/Home", component: ContainerMetricView };
