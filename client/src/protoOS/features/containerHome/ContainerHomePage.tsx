import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import ContainerMetricView from "./components/ContainerMetricView";
import ContainerOverviewView from "./components/ContainerOverviewView";
import ContainerSystemView from "./components/ContainerSystemView";
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
import { buildIssueSummary, buildModuleGrid, buildSummaryKpis } from "./utils/buildContainerSummary";
import { containerHistoryKey, type ContainerHistorySelection } from "@/protoOS/api/adapters/containerHistory";
import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePsuObservationTime } from "@/protoOS/hooks/usePsuObservationTime";
import { useDuration, useMinerStore, useSetDuration } from "@/protoOS/store";
import type { MetricTimeSeries } from "@/protoOS/store/contract";
import { useDetailedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";

const ORANGE = "var(--color-core-accent-fill)";
const COLORS = [ORANGE, "#783bf0", "#258fbf", "var(--color-core-primary-fill)", "#ff3344"];
const temperatureModes = [
  { id: "system" as const, label: "System" },
  { id: "hashboards" as const, label: "Hashboards" },
  { id: "fluid" as const, label: "Fluid" },
];

export default function ContainerHomePage({ tab }: { tab: HomeTab }) {
  const { minerRoot } = useMinerHosting();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const unit = useTemperatureUnit();
  const hardware = useMinerStore((state) => state.hardware);
  const telemetry = useMinerStore((state) => state.telemetry);
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedMode = searchParams.get("mode");
  const mode: HomeChartMode =
    tab === "temperature" && (requestedMode === "hashboards" || requestedMode === "fluid") ? requestedMode : "system";
  const errors = useDetailedErrors();
  const [issuesOpen, setIssuesOpen] = useState(false);
  const live = useMemo(() => buildContainerLive(hardware, telemetry, unit), [hardware, telemetry, unit]);
  // PSU samples expire between polls, so module states follow the clock as on the Tanks page.
  const now = usePsuObservationTime(tab === "overview");
  const grid = useMemo(
    () => (tab === "overview" ? buildModuleGrid(hardware, telemetry, errors, minerRoot, now) : undefined),
    [tab, hardware, telemetry, errors, minerRoot, now],
  );
  const issues = useMemo(() => buildIssueSummary(errors), [errors]);
  const fans = useMemo(
    () => [...hardware.fans.values()].filter((fan) => fan.ref.owner.kind === "container"),
    [hardware.fans],
  );
  const pdus = useMemo(
    () => [...hardware.plant.values()].filter((component) => component.kind === "distribution"),
    [hardware.plant],
  );
  const selections = useMemo<ContainerHistorySelection[]>(() => {
    if (tab === "system") return [];
    if (tab === "hashrate") return [{ metric: "hashrate" }];
    if (tab === "efficiency") return [{ metric: "hashrate" }, { metric: "facilityPower" }];
    if (tab === "overview")
      return [
        { metric: "hashrate" },
        { metric: "facilityPower" },
        { metric: "outletATemperature" },
        { metric: "outletBTemperature" },
      ];
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

  if (tab === "overview") {
    // One alert for the section, not one per chart.
    const history = (units: string, series: HomeHistory["series"]) => ({
      ...baseHistory,
      error: undefined,
      units,
      series,
    });
    const chart = (
      id: string,
      label: string,
      to: string,
      units: string,
      source: MetricTimeSeries | undefined,
      convert?: (value: number) => number,
    ) => {
      const series = toHomeSeries(id, label, ORANGE, source, window, units, convert);
      return { id, label, to, low: series.low ?? "—", high: series.high ?? "—", history: history(units, [series]) };
    };
    // Fluid outlet temperature: the plant has one sensor per tank bank, not per tank.
    const outlets = (
      [
        ["outletATemperature", "Left tanks"],
        ["outletBTemperature", "Right tanks"],
      ] as const
    ).map(([metric, label]) => ({ metric, label, source: source({ metric }) }));
    const outletRange = (key: "min" | "max") => {
      const values = outlets.flatMap(({ source }) => {
        const value = source?.aggregates?.[key]?.value;
        return typeof value === "number" && Number.isFinite(value) ? [value] : [];
      });
      const extreme = values.length ? (key === "min" ? Math.min(...values) : Math.max(...values)) : null;
      return formatReading(temperatureValue(extreme, unit), `°${unit}`);
    };
    const hashrateHistory = source({ metric: "hashrate" });
    const powerHistory = source({ metric: "facilityPower" });
    return (
      <>
        <ContainerOverviewView
          kpis={buildSummaryKpis(live)}
          production={[
            chart("hashrate", "Hashrate", `${minerRoot}/hashrate`, "TH/s", hashrateHistory),
            chart("power", "Power", `${minerRoot}/power-usage`, "kW", powerHistory, (value) => value / 1000),
            chart(
              "efficiency",
              "Efficiency",
              `${minerRoot}/efficiency`,
              "J/TH",
              efficiencyHistory(powerHistory, hashrateHistory),
            ),
            {
              id: "temperature",
              label: "Temperature",
              to: `${minerRoot}/temperature`,
              low: outletRange("min"),
              high: outletRange("max"),
              history: history(
                `°${unit}`,
                outlets.map(({ metric, label, source }) =>
                  toHomeSeries(metric, label, ORANGE, source, window, `°${unit}`, (value) =>
                    temperatureValue(value, unit)!,
                  ),
                ),
              ),
            },
          ]}
          historyError={historyState.error}
          duration={duration}
          onDurationChange={setDuration}
          grid={grid}
          issues={issues}
          links={{ tanks: `${minerRoot}/tanks` }}
          onViewIssues={() => setIssuesOpen(true)}
        />
        <ProtoOSStatusModal open={issuesOpen} onClose={() => setIssuesOpen(false)} />
      </>
    );
  }

  if (tab === "system") return <ContainerSystemView plant={live.plant} fans={fans} power={live.power} />;

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
