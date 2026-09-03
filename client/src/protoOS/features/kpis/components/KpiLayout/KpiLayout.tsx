import { useMemo } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import type { TimeSeriesLevel } from "@/protoOS/api/adapters/types";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { getNoPoolsCalloutState } from "@/protoOS/components/NoPoolsCallout/utility";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import { useDuration, useMinerStore, usePoolsInfo, useSetDuration } from "@/protoOS/store";
import DurationSelector from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const KpiLayout = () => {
  const poolsInfo = usePoolsInfo();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const isContainer = useMinerStore((state) => state.hardware.topology?.kind === "container");
  const { pathname } = useLocation();

  // Get latest miner level telemetry fo tabnav summary
  useTelemetry({ level: ["miner"] });

  // Memoize levels to prevent recreating on every render
  const levels = useMemo<TimeSeriesLevel[]>(() => {
    const miner: TimeSeriesLevel = {
      type: "miner",
      fields: ["hashrate", "power", "efficiency", "temperature"],
    };
    return isContainer
      ? [miner]
      : [miner, { type: "hashboard", fields: ["hashrate", "power", "efficiency", "temperature"] }];
  }, [isContainer]);

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
            <div className="grow text-heading-300">Home</div>
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
