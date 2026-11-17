import { useMemo } from "react";
import { Link, Navigate, Outlet, useLocation, useMatch } from "react-router-dom";
import { kpiFields, metricFromRouteParam, perBoardFieldsForMetric } from "./kpiMetricScoping";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import type { TelemetryLevel, TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { useNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/useNoPoolsCalloutState";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import { useKpiViewSelection } from "@/protoOS/features/kpis/kpiViewSelection";
import { useDuration, useMinerStore, usePoolsInfo, useSetDuration } from "@/protoOS/store";
import type { ContainerTopology, TankId, ViewSelection } from "@/protoOS/store/contract";
import { moduleHashboardIds } from "@/protoOS/store/hooks/kpiChartData";
import { tankRouteValue } from "@/protoOS/topologyRouteParams";
import DurationSelector from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const tankHashboardIds = (topology: ContainerTopology, tankId: TankId) =>
  topology.tanks.get(tankId)?.moduleIds.flatMap((moduleId) => topology.modules.get(moduleId)?.hashboardIds ?? []) ?? [];

const selectedContainerHashboardIndexes = (
  hardware: ReturnType<typeof useMinerStore.getState>["hardware"],
  selection: ViewSelection | undefined,
) => {
  const topology = hardware.topology;
  if (topology?.kind !== "container") return [];

  const selectedIds =
    selection?.level === "module"
      ? moduleHashboardIds(topology, selection.moduleId)
      : selection?.level === "tank"
        ? tankHashboardIds(topology, selection.tankId)
        : [...hardware.hashboards.keys()];

  const indexById = new Map([...hardware.hashboards.keys()].map((id, index) => [id, index]));
  return selectedIds.flatMap((hashboardId) => {
    const index = indexById.get(hashboardId);
    return index === undefined ? [] : [index];
  });
};

const KpiLayoutContent = ({ selection }: { selection: ViewSelection | undefined }) => {
  const poolsInfo = usePoolsInfo();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const isContainer = useMinerStore((state) => state.hardware.topology?.kind === "container");
  const hardware = useMinerStore((state) => state.hardware);
  const { minerRoot } = useMinerHosting();
  const { pathname } = useLocation();

  // The charted metric is the active tab's `:metric` route segment. Match the
  // route directly (Home tabs and embedded module tabs) rather than scanning the
  // pathname, so a metric word elsewhere in the path can't shadow the real tab.
  const homeTab = useMatch(`${minerRoot}/:metric`);
  const moduleTab = useMatch(`${minerRoot}/tanks/:tankId/modules/:moduleId/:metric`);
  const activeMetric = metricFromRouteParam((moduleTab ?? homeTab)?.params.metric);

  const heading = useMemo(() => {
    if (selection?.level !== "module") return "Home";
    const module = hardware.topology?.modules.get(selection.moduleId);
    return `Module ${module?.location?.moduleSlot ?? ""}`.trim();
  }, [hardware.topology, selection]);

  const breadcrumbs = useMemo(() => {
    if (selection?.level !== "module") return [];
    if (hardware.topology?.kind !== "container") return [];
    const module = hardware.topology?.modules.get(selection.moduleId);
    const tank = module?.tankId ? hardware.topology.tanks.get(module.tankId) : undefined;
    if (!tank) return [{ label: "Home", to: `${minerRoot}/` }, { label: heading }];

    const tankLabel = tank.label?.trim() || `Tank ${tank.slot ?? ""}`.trim();
    return [
      { label: "Home", to: `${minerRoot}/` },
      { label: tankLabel, to: `${minerRoot}/tanks/${tankRouteValue(tank)}` },
      { label: heading },
    ];
  }, [hardware.topology, heading, minerRoot, selection]);

  const telemetryLevels = useMemo<TelemetryLevel[]>(
    () => (selection?.level === "module" ? ["hashboard", "miner"] : ["miner"]),
    [selection],
  );

  // Get latest telemetry for the tabnav summary.
  useTelemetry({ level: telemetryLevels });

  // CT1 loads only the visible chart's aggregate and per-board histories.
  // Tab summaries use live telemetry; rigs can fetch all fields in one batch.
  const isContainerHome = isContainer && selection?.level !== "module" && selection?.level !== "tank";

  // Aggregate (miner-level) series. Container module/tank views have no
  // aggregate line (they read per-board only), matching prior behavior.
  const aggregateLevels = useMemo<TimeSeriesLevel[]>(() => {
    if (!isContainer) {
      return [
        { type: "miner", fields: kpiFields() },
        { type: "hashboard", fields: kpiFields() },
      ];
    }
    if (isContainer && !isContainerHome) return [];
    return [{ type: "miner", fields: perBoardFieldsForMetric(activeMetric) }];
  }, [isContainer, isContainerHome, activeMetric]);

  // Per-board (hashboard-level) series. On container Home this is every board;
  // on module/tank it is the selected subset. Feeds
  // per-tank chart lines and the Lowest Performer readout. Scoped to the active
  // tab's metric (not all four) since only one chart is on screen.
  // TODO(frigate): replace this per-board fan-out (one request per board) with a
  // container-wide / tank-level aggregated timeseries endpoint (see the
  // "Proposed API improvements" issue #88).
  const perBoardLevels = useMemo<TimeSeriesLevel[]>(() => {
    if (!isContainer) return [];
    const fields = perBoardFieldsForMetric(activeMetric);
    const indexes = selectedContainerHashboardIndexes(hardware, selection);
    return [{ type: "hashboard", fields, indexes }];
  }, [hardware, isContainer, selection, activeMetric]);

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
              {breadcrumbs.length ? (
                <nav
                  aria-label="Breadcrumb"
                  className="mb-2 flex flex-wrap items-center gap-2 text-300 text-text-primary-70"
                >
                  {breadcrumbs.map((breadcrumb, index) => (
                    <span key={`${breadcrumb.label}-${index}`} className="flex items-center gap-2">
                      {breadcrumb.to ? (
                        <Link className="hover:text-text-primary" to={breadcrumb.to}>
                          {breadcrumb.label}
                        </Link>
                      ) : (
                        <span className="text-text-primary">{breadcrumb.label}</span>
                      )}
                      {index < breadcrumbs.length - 1 ? <span>/</span> : null}
                    </span>
                  ))}
                </nav>
              ) : null}
              <div className="text-heading-300">{heading}</div>
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

const KpiLayout = () => {
  const selection = useKpiViewSelection();
  const { minerRoot } = useMinerHosting();

  if (selection?.level === "notFound") return <Navigate to={`${minerRoot}/`} replace />;

  return <KpiLayoutContent selection={selection} />;
};

export default KpiLayout;
