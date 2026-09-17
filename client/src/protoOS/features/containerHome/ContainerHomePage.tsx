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
  latest,
  sumHistory,
  temperatureValue,
  toHomeSeries,
} from "./utils/buildContainerHome";
import { containerHistoryKey, type ContainerHistorySelection } from "@/protoOS/api/adapters/containerHistory";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useDuration, useMinerStore } from "@/protoOS/store";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";

const ORANGE = "var(--color-core-accent-fill)";
const COLORS = [ORANGE, "#783bf0", "#258fbf", "var(--color-core-primary-fill)", "#ff3344"];
const powerModes = [
  { id: "system" as const, label: "System" },
  { id: "hashboards" as const, label: "Hashboards" },
];
const temperatureModes = [...powerModes, { id: "fluid" as const, label: "Fluid" }];

export default function ContainerHomePage({ tab }: { tab: HomeTab }) {
  const { minerRoot } = useMinerHosting();
  const duration = useDuration();
  const unit = useTemperatureUnit();
  const hardware = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) => state.telemetry);
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedMode = searchParams.get("mode");
  const mode: HomeChartMode =
    requestedMode === "hashboards" || (requestedMode === "fluid" && tab === "temperature") ? requestedMode : "system";
  const live = useMemo(() => buildContainerLive(hardware, telemetry, unit), [hardware, telemetry, unit]);
  const fans = useMemo(
    () => [...hardware.fans.values()].filter((fan) => fan.ref.owner.kind === "container"),
    [hardware.fans],
  );
  const selections = useMemo<ContainerHistorySelection[]>(() => {
    if (tab === "overview" || tab === "hashrate") return [{ metric: "hashrate" }];
    if (tab === "efficiency") return [{ metric: "hashrate" }, { metric: "facilityPower" }];
    if (mode !== "system") return [];
    if (tab === "power")
      return [
        { metric: "power" },
        ...fans.flatMap((fan) => (fan.name ? [{ metric: "fanPower" as const, identifier: fan.name }] : [])),
      ];
    return [
      { metric: "temperature" },
      { metric: "insideTemperature" },
      { metric: "outsideTemperature" },
      { metric: "inletTemperature" },
      { metric: "outletTemperature" },
    ];
  }, [tab, mode, fans]);
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

  if (tab === "overview")
    return (
      <ContainerOverviewView
        hashrate={{ id: "hashrate", label: "Hashrate", value: formatReading(live.hashrate, "TH/s") }}
        power={{
          id: "power",
          label: "Power",
          value: formatReading(live.power === null ? null : live.power / 1000, "kW"),
        }}
        fluid={{
          id: "fluid",
          label: "Fluid levels and temperature",
          value: formatReading(temperatureValue(latest(live.plant?.inletTemperature), unit), `°${unit}`),
        }}
        boardCount={live.boardCount}
        powerLocations={live.powerLocations}
        fluidLocations={live.fluidLocations}
        history={{
          ...baseHistory,
          units: "TH/s",
          series: [toHomeSeries("hashrate", "Hashrate", ORANGE, source({ metric: "hashrate" }), window, "TH/s")],
        }}
        links={{
          hashrate: `${minerRoot}/hashrate`,
          power: `${minerRoot}/power-usage`,
          fluid: `${minerRoot}/temperature?mode=fluid`,
        }}
      />
    );

  if (tab === "power") {
    const series =
      mode === "system"
        ? [
            toHomeSeries(
              "mining",
              "Mining power",
              ORANGE,
              source({ metric: "power" }),
              window,
              "kW",
              (value) => value / 1000,
            ),
            toHomeSeries("pumps", "Pumps", COLORS[1], undefined, window, "kW"),
            fanSeries,
            toHomeSeries("ac", "AC", COLORS[3], undefined, window, "kW"),
            toHomeSeries("aux", "Aux", COLORS[4], undefined, window, "kW"),
          ]
        : [];
    return (
      <ContainerMetricView
        title="Power"
        readings={live.powerReadings}
        mode={mode}
        modes={powerModes}
        onModeChange={onModeChange}
        history={{
          ...baseHistory,
          series,
        }}
        categories={categorySummaries(fanSeries)}
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
          label: `Container ${tab}`,
          value: formatReading(efficiency ? live.efficiency : live.hashrate, units),
        },
        { id: "boards", label: "Hashboards", value: live.boardCount ?? "—" },
      ]}
      history={{ ...baseHistory, units, series: [toHomeSeries(tab, title, ORANGE, series, window, units)] }}
    />
  );
}
