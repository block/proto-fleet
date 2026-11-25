import { useMemo } from "react";
import { Outlet, useLocation, useMatch } from "react-router-dom";
import { kpiFields, metricFromRouteParam, perBoardFieldsForMetric } from "./kpiMetricScoping";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import type { TelemetryLevel, TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { useNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/useNoPoolsCalloutState";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import { useDuration, useMinerStore, usePoolsInfo, useSetDuration } from "@/protoOS/store";
import DurationSelector from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const telemetryLevels: TelemetryLevel[] = ["miner"];

const containerHashboardIndexes = (hardware: ReturnType<typeof useMinerStore.getState>["hardware"]) =>
  hardware.topology?.kind === "container" ? Array.from(hardware.hashboards.keys(), (_, index) => index) : [];

const KpiLayout = () => {
  const poolsInfo = usePoolsInfo();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const isContainer = useMinerStore((state) => state.hardware.topology?.kind === "container");
  const hardware = useMinerStore((state) => state.hardware);
  const { minerRoot } = useMinerHosting();
  const { pathname } = useLocation();

  // The charted metric is the active tab's `:metric` route segment. Match the
  // route directly rather than scanning the pathname, so a metric word
  // elsewhere in the path can't shadow the real tab.
  const homeTab = useMatch(`${minerRoot}/:metric`);
  const activeMetric = metricFromRouteParam(homeTab?.params.metric);

  // Get latest telemetry for the tabnav summary.
  useTelemetry({ level: telemetryLevels });

  // CT1 loads only the visible chart's aggregate and per-board histories.
  // Tab summaries use live telemetry; rigs can fetch all fields in one batch.
  const aggregateLevels = useMemo<TimeSeriesLevel[]>(() => {
    if (!isContainer) {
      return [
        { type: "miner", fields: kpiFields() },
        { type: "hashboard", fields: kpiFields() },
      ];
    }
    return [{ type: "miner", fields: perBoardFieldsForMetric(activeMetric) }];
  }, [isContainer, activeMetric]);

  // Per-board (hashboard-level) series for every board of a container. Feeds
  // per-tank chart lines and the Lowest Performer readout. Scoped to the active
  // tab's metric (not all four) since only one chart is on screen.
  // TODO(frigate): replace this per-board fan-out (one request per board) with a
  // container-wide / tank-level aggregated timeseries endpoint.
  const perBoardLevels = useMemo<TimeSeriesLevel[]>(() => {
    if (!isContainer) return [];
    const fields = perBoardFieldsForMetric(activeMetric);
    const indexes = containerHashboardIndexes(hardware);
    return [{ type: "hashboard", fields, indexes }];
  }, [hardware, isContainer, activeMetric]);

  useTimeSeries({
    duration,
    levels: aggregateLevels,
    poll: true,
    enabled: aggregateLevels.length > 0,
  });
  useTimeSeries({
    duration,
    levels: perBoardLevels,
    poll: true,
    enabled: perBoardLevels.length > 0,
  });

  const { arePoolsConfigured, shouldShowNoPoolsCallout } = useNoPoolsCalloutState(poolsInfo, pathname);

  return (
    <ErrorBoundary>
      <div className="p-6 tablet:p-10 laptop:p-14">
        {shouldShowNoPoolsCallout ? <NoPoolsCallout arePoolsConfigured={arePoolsConfigured} /> : null}

        <div className="relative flex h-[calc(100vh-theme(spacing.36))] min-h-[800px] flex-col phone:min-h-[1000px]">
          <div className="flex items-center pb-6">
            <div className="grow">
              <div className="text-heading-300">Home</div>
            </div>
            <DurationSelector className="h-fit" duration={duration} onSelect={setDuration} />
          </div>

          <div className="pb-6">
            <TabMenu />
          </div>
          <ErrorBoundary>
            <Outlet />
          </ErrorBoundary>
        </div>
      </div>
    </ErrorBoundary>
  );
};

export default KpiLayout;
