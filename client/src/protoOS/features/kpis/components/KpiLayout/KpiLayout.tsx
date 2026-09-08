import { useMemo } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import type { TelemetryLevel, TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { getNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/utility";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import { useKpiViewSelection } from "@/protoOS/features/kpis/kpiViewSelection";
import { useDuration, useMinerStore, usePoolsInfo, useSetDuration } from "@/protoOS/store";
import DurationSelector from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const KpiLayout = () => {
  const poolsInfo = usePoolsInfo();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const selection = useKpiViewSelection();
  const isContainer = useMinerStore((state) => state.hardware.topology?.kind === "container");
  const hardware = useMinerStore((state) => state.hardware);
  const { pathname } = useLocation();

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
      fields: ["hashrate", "power", "efficiency", "temperature"],
    };
    if (isContainer && selection?.level === "module") {
      const module = hardware.topology?.modules.get(selection.moduleId);
      const hashboardIndexById = new Map([...hardware.hashboards.keys()].map((id, index) => [id, index]));
      const indexes =
        module?.hashboardIds.flatMap((hashboardId) => {
          const index = hashboardIndexById.get(hashboardId);
          return index === undefined ? [] : [index];
        }) ?? [];
      return indexes.length
        ? [{ type: "hashboard", fields: ["hashrate", "power", "efficiency", "temperature"], indexes }]
        : [];
    }

    const hashboards: TimeSeriesLevel = {
      type: "hashboard",
      fields: ["hashrate", "power", "efficiency", "temperature"],
      indexes: isContainer ? Array.from({ length: hardware.hashboards.size }, (_, index) => index) : undefined,
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
            <div className="grow text-heading-300">
              {selection?.level === "module"
                ? `Module ${hardware.topology?.modules.get(selection.moduleId)?.location?.moduleSlot ?? ""}`.trim()
                : "Home"}
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
