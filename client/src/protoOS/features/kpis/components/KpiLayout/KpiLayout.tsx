import { useEffect, useMemo, useState } from "react";
import { Outlet } from "react-router-dom";
import { useHashboards } from "@/protoOS/api";
import { ContentLayoutProps } from "@/protoOS/components/ContentLayout/types";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import {
  useProcessedEfficiency,
  useProcessedHashrate,
  useProcessedPowerUsage,
  useProcessedTemperature,
} from "@/protoOS/features/kpis/hooks";
import { type KpiOutletContext } from "@/protoOS/features/kpis/types";
import DurationSelector, {
  Duration,
  durations,
} from "@/shared/components/DurationSelector";
import ErrorBoundary from "@/shared/components/ErrorBoundary";
import ProgressCircular from "@/shared/components/ProgressCircular";
import NoPoolsCallout from "@/shared/features/kpis/components/NoPoolsCallout";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const KpiLayout = ({ children }: ContentLayoutProps) => {
  const { getItem, setItem } = useLocalStorage();
  const [hashboardSerials, setHashboardSerials] = useState<string[]>();

  const { data: hashboardsInfo } = useHashboards();
  const { poolsInfo, poolsInfoStatus } = useMinerStatus();
  const [duration, setDuration] = useState<Duration>(
    getItem("duration") || durations[0],
  );
  const [outletContext, setOutletContext] = useState<KpiOutletContext | null>();
  const minerHashrate = useProcessedHashrate({ duration });
  const minerEfficiency = useProcessedEfficiency({ duration });
  const minerPowerUsage = useProcessedPowerUsage({ duration });
  const minerTemperature = useProcessedTemperature({ duration });

  useEffect(() => {
    setItem("duration", duration);
  }, [duration, setItem]);

  useEffect(() => {
    if (hashboardsInfo) {
      setHashboardSerials(
        hashboardsInfo
          ?.sort(
            (a, b) =>
              (a.slot || hashboardsInfo.length) -
              (b.slot || hashboardsInfo.length),
          )
          .map((hashboardInfo) => hashboardInfo.hb_sn)
          .filter(Boolean) as string[],
      );
    }
  }, [hashboardsInfo]);

  useEffect(() => {
    if (!hashboardSerials || !duration) return;

    setOutletContext({
      duration,
      hashboardSerials,
      minerHashrate,
      minerTemperature,
      minerPowerUsage,
      minerEfficiency,
    });
  }, [
    duration,
    hashboardSerials,
    minerHashrate,
    minerTemperature,
    minerPowerUsage,
    minerEfficiency,
  ]);

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

        <div className="relative mb-4 flex h-[calc(100vh-theme(spacing.36))] min-h-[800px] flex-col phone:min-h-[1000px]">
          <div className="flex items-center pb-6">
            <div className="grow text-heading-300">Home</div>
            <DurationSelector
              className="h-fit"
              duration={duration}
              onSelect={setDuration}
            />
          </div>

          <div className="pb-6 phone:pb-6">
            <TabMenu
              hashrate={
                minerHashrate?.hashrate[minerHashrate?.hashrate?.length - 1]
                  ?.value
              }
              efficiency={
                minerEfficiency?.efficiency[
                  minerEfficiency?.efficiency?.length - 1
                ]?.value
              }
              powerUsage={
                minerPowerUsage?.powerUsage[
                  minerPowerUsage?.powerUsage?.length - 1
                ]?.value
              }
              temperature={
                minerTemperature?.temperature[
                  minerTemperature?.temperature?.length - 1
                ]?.value
              }
            />
          </div>

          {outletContext ? (
            <ErrorBoundary>
              <Outlet context={outletContext} />
            </ErrorBoundary>
          ) : (
            <div className="flex h-full flex-1 items-center justify-center">
              <ProgressCircular indeterminate />
            </div>
          )}
        </div>
      </div>
    </ErrorBoundary>
  );
};

export default KpiLayout;
