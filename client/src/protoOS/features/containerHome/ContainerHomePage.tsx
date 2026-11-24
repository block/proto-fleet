import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import ContainerMetricView from "./components/ContainerMetricView";
import ContainerOverviewView from "./components/ContainerOverviewView";
import ContainerSystemView from "./components/ContainerSystemView";
import { useContainerHistory } from "./hooks/useContainerHistory";
import type { HomeChartMode, HomeHistory, HomeTab } from "./types";
import {
  buildContainerLive,
  efficiencyHistory,
  formatReading,
  latest,
  sumHistory,
  temperatureValue,
  toHomeSeries,
} from "./utils/buildContainerHome";
import { buildIssueSummary, buildModuleGrid } from "./utils/buildContainerSummary";
import { containerHistoryKey, type ContainerHistorySelection } from "@/protoOS/api/adapters/containerHistory";
import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import ModuleModal from "@/protoOS/features/tanks/components/ModuleModal/ModuleModal";
import { usePsuObservationTime } from "@/protoOS/hooks/usePsuObservationTime";
import { useDuration, useMinerStore, useSetDuration } from "@/protoOS/store";
import type { MetricTimeSeries, ModuleNode } from "@/protoOS/store/contract";
import { useDetailedErrors } from "@/protoOS/store/hooks/useMinerStatus";
import { useTemperatureUnit } from "@/protoOS/store/hooks/useUI";
import { formatHashrateWithUnit } from "@/shared/utils/telemetryFormat";

const ORANGE = "var(--color-core-accent-fill)";
const COLORS = [ORANGE, "#783bf0", "#258fbf", "var(--color-core-primary-fill)", "#ff3344"];
const temperatureModes = [
  { id: "system" as const, label: "System" },
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
  const mode: HomeChartMode = tab === "temperature" && requestedMode === "fluid" ? "fluid" : "system";
  const errors = useDetailedErrors();
  const [issuesOpen, setIssuesOpen] = useState(false);
  const [selectedLocation, setSelectedLocation] = useState<{
    containerId: string;
    tankSlot: number;
    moduleSlot: number;
  }>();
  const topology = hardware.topology;
  const selectedTank =
    topology?.kind === "container" && topology.containerId === selectedLocation?.containerId
      ? [...topology.tanks.values()].find((tank) => tank.slot === selectedLocation.tankSlot)
      : undefined;
  const selectedModule =
    topology?.kind === "container" && selectedTank
      ? [...topology.modules.values()].find(
          (module) => module.tankId === selectedTank.id && module.location?.moduleSlot === selectedLocation?.moduleSlot,
        )
      : undefined;
  // Clear removed locations before rendering; a later map must not reopen them.
  if (selectedLocation && !selectedModule) setSelectedLocation(undefined);
  const selectModule = (module: ModuleNode | undefined) => {
    const tank = topology?.kind === "container" && module?.tankId ? topology.tanks.get(module.tankId) : undefined;
    setSelectedLocation(
      topology?.kind === "container" && tank?.slot !== undefined && module?.location?.moduleSlot !== undefined
        ? { containerId: topology.containerId, tankSlot: tank.slot, moduleSlot: module.location.moduleSlot }
        : undefined,
    );
  };
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
    if (tab === "overview") return [{ metric: "hashrate" }, { metric: "facilityPower" }, { metric: "temperature" }];
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
      units: string,
      source: MetricTimeSeries | undefined,
      value: number | null,
      convert?: (value: number) => number,
    ) => {
      const series = toHomeSeries(id, label, ORANGE, source, window, units, convert);
      return {
        id,
        label,
        // Direct metrics retain API bucket extrema. Efficiency is derived from
        // bucket averages; its extrema summarize those calculated ratios.
        stats: {
          label,
          value: formatReading(value, units),
          preformatted: true,
          valueSuffix: `Max ${series.high} · Min ${series.low}`,
        },
        history: history(units, [series]),
      };
    };
    const hashrateHistory = source({ metric: "hashrate" });
    const powerHistory = source({ metric: "facilityPower" });
    const rateReference = live.hashrate ?? hashrateHistory?.aggregates?.max?.value ?? 0;
    const rateScale = formatHashrateWithUnit(rateReference);
    const rateFactor = rateReference > 0 ? rateScale.value / rateReference : 1;
    const rateUnits = rateScale.unit.replace("/S", "/s");
    return (
      <>
        <ContainerOverviewView
          production={[
            chart(
              "hashrate",
              "Hashrate",
              rateUnits,
              hashrateHistory,
              live.hashrate === null ? null : live.hashrate * rateFactor,
              (value) => value * rateFactor,
            ),
            {
              ...chart(
                "temperature",
                "Average hashboard temp",
                `°${unit}`,
                source({ metric: "temperature" }),
                temperatureValue(latest(live.rollup?.temperature), unit),
                (value) => temperatureValue(value, unit)!,
              ),
              label: "Temperature",
            },
            chart(
              "power",
              "Power",
              "kW",
              powerHistory,
              live.power === null ? null : live.power / 1000,
              (value) => value / 1000,
            ),
            chart(
              "efficiency",
              "Efficiency",
              "J/TH",
              efficiencyHistory(powerHistory, hashrateHistory),
              live.efficiency,
            ),
          ]}
          historyError={historyState.error}
          duration={duration}
          onDurationChange={setDuration}
          grid={grid}
          onSelectModule={(id) =>
            selectModule(
              topology?.kind === "container"
                ? [...topology.modules.values()].find((module) => module.id === id)
                : undefined,
            )
          }
          issues={issues}
          onViewIssues={() => setIssuesOpen(true)}
        />
        {selectedModule && selectedTank && topology?.kind === "container" ? (
          <ModuleModal
            module={selectedModule}
            tank={selectedTank}
            topology={topology}
            onClose={() => setSelectedLocation(undefined)}
            onSelectModule={selectModule}
            keepHashboardsInModal
          />
        ) : null}
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
      />
    );
  }

  if (tab === "temperature") {
    const sensorSeries = (
      [
        ["temperature", "Hashboard avg", COLORS[0]],
        ["inletTemperature", "Fluid inlet", COLORS[2]],
        ["outletTemperature", "Fluid outlet", COLORS[4]],
        ["outsideTemperature", "Ambient outdoor", COLORS[3]],
        ["insideTemperature", "Ambient indoor", COLORS[1]],
      ] as const
    ).map(([metric, label, color]) =>
      toHomeSeries(metric, label, color, source({ metric }), window, `°${unit}`, (value) =>
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
