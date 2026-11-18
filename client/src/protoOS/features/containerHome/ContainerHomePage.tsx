import { useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import ContainerMetricView from "./components/ContainerMetricView";
import ContainerOverviewView from "./components/ContainerOverviewView";
import { useContainerHistory } from "./hooks/useContainerHistory";
import type { HomeChartMode, HomeHistory, HomeTab } from "./types";
import {
  buildContainerLive,
  categorySummaries,
  efficiencyHistory,
  formatReading,
  sumHistory,
  temperatureValue,
  toHomeSeries,
} from "./utils/buildContainerHome";
import { containerHistoryKey, type ContainerHistorySelection } from "@/protoOS/api/adapters/containerHistory";
import { useDuration, useMinerStore } from "@/protoOS/store";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";

const ORANGE = "var(--color-core-accent-fill)";
const COLORS = [ORANGE, "#783bf0", "#258fbf", "var(--color-core-primary-fill)", "#ff3344"];
const temperatureModes = [
  { id: "system" as const, label: "System" },
  { id: "hashboards" as const, label: "Hashboards" },
  { id: "fluid" as const, label: "Fluid" },
];

export default function ContainerHomePage({ tab }: { tab: HomeTab }) {
  const duration = useDuration();
  const unit = useTemperatureUnit();
  const hardware = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) => state.telemetry);
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedMode = searchParams.get("mode");
  const mode: HomeChartMode =
    tab === "temperature" && (requestedMode === "hashboards" || requestedMode === "fluid") ? requestedMode : "system";
  const live = useMemo(() => buildContainerLive(hardware, telemetry, unit), [hardware, telemetry, unit]);
  const fans = useMemo(
    () => [...hardware.fans.values()].filter((fan) => fan.ref.owner.kind === "container"),
    [hardware.fans],
  );
  const pdus = useMemo(
    () => [...hardware.plant.values()].filter((component) => component.kind === "distribution"),
    [hardware.plant],
  );
  const selections = useMemo<ContainerHistorySelection[]>(() => {
    if (tab === "overview") return [];
    if (tab === "hashrate") return [{ metric: "hashrate" }];
    if (tab === "efficiency") return [{ metric: "hashrate" }, { metric: "facilityPower" }];
    if (mode !== "system") return [];
    if (tab === "power")
      return [
        { metric: "facilityPower" },
        { metric: "power" },
        ...pdus.map((pdu) => ({ metric: "distributionPower" as const, identifier: pdu.name })),
        ...fans.flatMap((fan) => (fan.name ? [{ metric: "fanPower" as const, identifier: fan.name }] : [])),
      ];
    return [
      { metric: "temperature" },
      { metric: "insideTemperature" },
      { metric: "outsideTemperature" },
      { metric: "inletTemperature" },
      { metric: "outletTemperature" },
    ];
  }, [tab, mode, fans, pdus]);
  const historyState = useContainerHistory(duration, selections);
  const window = historyState.window ?? ([0, 0] as [number, number]);
  const source = (selection: ContainerHistorySelection) => historyState.result?.series[containerHistoryKey(selection)];
  const fanSeries = toHomeSeries(
    "fans",
    "Fans",
    COLORS[2],
    sumHistory(fans.map((fan) => (fan.name ? source({ metric: "fanPower", identifier: fan.name }) : undefined))),
    window,
    "kW",
    (value) => value / 1000,
  );
  const baseHistory: HomeHistory = {
    series: [],
    units: "kW",
    window,
    loading: historyState.pending,
    error: historyState.error,
  };
  const onModeChange = (next: HomeChartMode) =>
    setSearchParams((previous) => {
      const params = new URLSearchParams(previous);
      if (next === "system") params.delete("mode");
      else params.set("mode", next);
      return params;
    });

  if (tab === "overview") return <ContainerOverviewView plant={live.plant} fans={fans} power={live.power} />;

  if (tab === "power") {
    const series = [
      toHomeSeries(
        "facility",
        "Facility total",
        COLORS[3],
        source({ metric: "facilityPower" }),
        window,
        "kW",
        (value) => value / 1000,
      ),
      toHomeSeries(
        "mining",
        "Mining power",
        ORANGE,
        source({ metric: "power" }),
        window,
        "kW",
        (value) => value / 1000,
      ),
      ...pdus.map((pdu, index) =>
        toHomeSeries(
          pdu.ref.id,
          pdu.name,
          `hsl(${(270 + index * 137.508) % 360} 65% 50%)`,
          source({ metric: "distributionPower", identifier: pdu.name }),
          window,
          "kW",
          (value) => value / 1000,
        ),
      ),
      fanSeries,
    ];
    return (
      <ContainerMetricView
        title="Power"
        readings={live.powerReadings}
        history={{
          ...baseHistory,
          series,
        }}
        categories={[
          {
            id: fanSeries.id,
            label: fanSeries.label,
            high: fanSeries.high ?? "—",
            low: fanSeries.low ?? "—",
            average: fanSeries.average ?? "—",
          },
        ]}
      />
    );
  }

  if (tab === "temperature") {
    const sensorSeries = (
      [
        ["temperature", "Hashboards"],
        ["insideTemperature", "Ambient"],
        ["outsideTemperature", "Outdoor"],
        ["inletTemperature", "Fluid inlet"],
        ["outletTemperature", "Fluid outlet"],
      ] as const
    ).map(([metric, label], index) =>
      toHomeSeries(metric, label, COLORS[index], source({ metric }), window, `°${unit}`, (value) =>
        temperatureValue(value, unit)!,
      ),
    );
    return (
      <ContainerMetricView
        title="Temperature"
        readings={live.temperatureReadings}
        mode={mode}
        modes={temperatureModes}
        onModeChange={onModeChange}
        history={{
          ...baseHistory,
          units: `°${unit}`,
          series: mode === "system" ? sensorSeries : [],
        }}
        locations={live.fluidLocations}
        categories={categorySummaries()}
      />
    );
  }

  const efficiency = tab === "efficiency";
  const title = efficiency ? "Efficiency" : "Hashrate";
  const units = efficiency ? "J/TH" : "TH/s";
  const series = efficiency
    ? efficiencyHistory(source({ metric: "facilityPower" }), source({ metric: "hashrate" }))
    : source({ metric: "hashrate" });
  return (
    <ContainerMetricView
      title={title}
      readings={[
        {
          id: tab,
          label: efficiency ? "Container efficiency" : "Hashrate",
          value: formatReading(efficiency ? live.efficiency : live.hashrate, units),
        },
        { id: "boards", label: "Hashboards", value: live.boardCount ?? "—" },
      ]}
      history={{ ...baseHistory, units, series: [toHomeSeries(tab, title, ORANGE, series, window, units)] }}
    />
  );
}
