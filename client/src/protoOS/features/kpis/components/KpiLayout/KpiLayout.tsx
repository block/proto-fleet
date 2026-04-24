import { useMemo } from "react";
import { Outlet } from "react-router-dom";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import { HashboardFieldType, MinerFieldType } from "@/protoOS/api/generatedApi";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import { usePoolsInfo } from "@/protoOS/store";
import { useDuration, useSetDuration } from "@/protoOS/store";
import DurationSelector from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const KpiLayout = () => {
  const poolsInfo = usePoolsInfo();
  const duration = useDuration();
  const setDuration = useSetDuration();

  // Get latest miner level telemetry fo tabnav summary
  useTelemetry({ level: ["miner"] });

  // Memoize levels to prevent recreating on every render
  const levels = useMemo(
    () => [
      {
        type: "miner" as const,
        fields: [MinerFieldType.Hashrate, MinerFieldType.Power, MinerFieldType.Efficiency, MinerFieldType.Temperature],
      },
      {
        type: "hashboard" as const,
        fields: [
          HashboardFieldType.Hashrate,
          HashboardFieldType.Power,
          HashboardFieldType.Efficiency,
          HashboardFieldType.Temperature,
        ],
      },
    ],
    [],
  );

  // Fetch all time series data here for hashrate/eff/power/temperature in one request
  // Used by multiple KPI tabs
  useTimeSeries({
    duration,
    levels,
    poll: true,
  });

  const noPoolsLive = useMemo(() => {
    return (
      poolsInfo !== undefined &&
      // TODO: remove alive when cgminer is removed
      !poolsInfo?.find((pool) => /alive|active/i.test(pool?.status ?? ""))
    );
  }, [poolsInfo]);

  return (
    <ErrorBoundary>
      <div className="p-14 phone:p-6 tablet:p-10">
        {noPoolsLive ? <NoPoolsCallout arePoolsConfigured={!!poolsInfo?.[0]?.url} /> : null}

        <div className="relative flex h-[calc(100vh-theme(spacing.36))] min-h-[800px] flex-col phone:min-h-[1000px]">
          <div className="flex items-center pb-6">
            <div className="grow text-heading-300">Home</div>
            <DurationSelector className="h-fit" duration={duration} onSelect={setDuration} />
          </div>

          <div className="pb-6 phone:pb-6">
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
