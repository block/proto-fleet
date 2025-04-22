import { useEffect, useMemo, useState } from "react";
import { Outlet } from "react-router-dom";
import NoPoolsCallout from "../NoPoolsCallout";
import { useHashboards } from "@/protoOS/api";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import {
  useProcessedEfficiency,
  useProcessedHashrate,
  useProcessedPowerUsage,
  useProcessedTemperature,
} from "@/protoOS/features/kpis/hooks";
import { type OutletContext } from "@/protoOS/features/kpis/types";
import DurationSelector, {
  Duration,
  durations,
} from "@/shared/components/DurationSelector";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const KpiLayout = () => {
  const { getItem, setItem } = useLocalStorage();
  const [hashboardSerials, setHashboardSerials] = useState<string[]>();

  // set HashboardSerials to local storage

  const { data: hashboardsInfo } = useHashboards();
  const { poolsInfo, poolsInfoStatus } = useMinerStatus();
  const [duration, setDuration] = useState<Duration>(
    getItem("duration") || durations[0],
  );
  const [outletContext, setOutletContext] = useState<OutletContext | null>();
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
          ?.map((hashboardInfo) => hashboardInfo.hb_sn)
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
    <div className="px-14 pt-14 phone:px-6 phone:pt-6 tablet:px-10 tablet:pt-10">
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

        <div className="pb-11 phone:pb-6">
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
          <Outlet context={outletContext} />
        ) : (
          <div className="flex h-full flex-1 items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        )}
      </div>
    </div>
  );
};

export default KpiLayout;
