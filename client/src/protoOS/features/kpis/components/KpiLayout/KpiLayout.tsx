import { useMemo } from "react";
import { Outlet } from "react-router-dom";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import { HashboardFieldType, MinerFieldType } from "@/protoOS/api/generatedApi";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import NoPoolsCallout from "@/protoOS/components/NoPoolsCallout";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import { useDuration, useSetDuration } from "@/protoOS/store";
import DurationSelector from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";

const KpiLayout = ({ children }: ContentLayoutProps) => {
  const { poolsInfo, poolsInfoStatus } = useMinerStatus();
  const duration = useDuration();
  const setDuration = useSetDuration();

  // Get latest miner level telemetry fo tabnav summary
  useTelemetry({ level: ["miner"] });

  // Memoize levels to prevent recreating on every render
  const levels = useMemo(
    () => [
      {
        type: "miner" as const,
        fields: [
          MinerFieldType.Hashrate,
          MinerFieldType.Power,
          MinerFieldType.Efficiency,
        ],
      },
      {
        type: "hashboard" as const,
        fields: [
          HashboardFieldType.Hashrate,
          HashboardFieldType.Power,
          HashboardFieldType.Efficiency,
        ],
      },
    ],
    [],
  );

  // Fetch all time series data here for hashrate/eff/power in one request
  // Used by multiple KPI tabs. Temperature uses latest values for asics so not included here
  useTimeSeries({
    duration,
    levels,
    poll: true,
  });

  const noPoolsLive = useMemo(() => {
    return (
      !poolsInfoStatus.pending &&
      !poolsInfoStatus.error &&
      // TODO: remove alive when cgminer is removed
      !poolsInfo?.find((pool) => /alive|active/i.test(pool?.status ?? ""))
    );
  }, [poolsInfo, poolsInfoStatus]);

  return (
    <ErrorBoundary>
      <div className="px-14 pt-14 phone:px-6 phone:pt-6 tablet:px-10 tablet:pt-10">
        {children}

        {noPoolsLive && (
          <NoPoolsCallout arePoolsConfigured={!!poolsInfo?.[0]?.url} />
        )}

        <div className="relative flex h-[calc(100vh-theme(spacing.36))] min-h-[800px] flex-col phone:min-h-[1000px]">
          <div className="flex items-center pb-6">
            <div className="grow text-heading-300">Home</div>
            <DurationSelector
              className="h-fit"
              duration={duration}
              onSelect={setDuration}
            />
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
