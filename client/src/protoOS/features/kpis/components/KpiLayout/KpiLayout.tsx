import { useMemo } from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import type { TelemetryLevel, TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { getNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/utility";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import { useKpiViewSelection } from "@/protoOS/features/kpis/kpiViewSelection";
import { useDuration, useMinerStore, usePoolsInfo, useSetDuration } from "@/protoOS/store";
import type { ContainerTopology, TankId, ViewSelection } from "@/protoOS/store/contract";
import { moduleHashboardIds } from "@/protoOS/store/hooks/kpiChartData";
import { tankRouteValue } from "@/protoOS/topologyRouteParams";
import DurationSelector from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

type KpiTimeSeriesField = Extract<TimeSeriesLevel, { type: "miner" }>["fields"][number];

const kpiFields = (): KpiTimeSeriesField[] => ["hashrate", "power", "efficiency", "temperature"];

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

const KpiLayout = () => {
  const poolsInfo = usePoolsInfo();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const selection = useKpiViewSelection();
  const isContainer = useMinerStore((state) => state.hardware.topology?.kind === "container");
  const hardware = useMinerStore((state) => state.hardware);
  const { minerRoot } = useMinerHosting();
  const { pathname } = useLocation();

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

  // Memoize levels to prevent recreating on every render
  const levels = useMemo<TimeSeriesLevel[]>(() => {
    const miner: TimeSeriesLevel = {
      type: "miner",
      fields: kpiFields(),
    };
    if (isContainer) {
      const indexes = selectedContainerHashboardIndexes(hardware, selection);
      const hashboard: TimeSeriesLevel = { type: "hashboard", fields: kpiFields(), indexes };
      return selection?.level === "module" || selection?.level === "tank" ? [hashboard] : [miner, hashboard];
    }

    const hashboards: TimeSeriesLevel = {
      type: "hashboard",
      fields: kpiFields(),
    };
    return [miner, hashboards];
  }, [hardware, isContainer, selection]);

  // Fetch all time series data here for hashrate/eff/power/temperature in one request
  // Used by multiple KPI tabs
  useTimeSeries({
    duration,
    levels,
    poll: true,
  });

  const { arePoolsConfigured, shouldShowNoPoolsCallout } = useMemo(
    () => getNoPoolsCalloutState(poolsInfo, pathname),
    [poolsInfo, pathname],
  );

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

export default KpiLayout;
