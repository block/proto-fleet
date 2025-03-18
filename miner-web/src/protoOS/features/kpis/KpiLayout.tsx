import { useEffect, useMemo, useState } from "react";
import { Outlet } from "react-router-dom";
import {
  useProcessedEfficiency,
  useProcessedHashrate,
  useProcessedPowerUsage,
  useProcessedTemperature,
} from "./hooks";
import NoPoolsCallout from "./NoPoolsCallout";
import { type OutletContext } from "./types";
import { useHashboards } from "@/protoOS/api";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import TabMenu from "@/protoOS/features/kpis/TabMenu";
import DurationSelector, {
  Duration,
  durations,
} from "@/shared/components/DurationSelector";
import Spinner from "@/shared/components/Spinner";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const KpiLayout = () => {
  const { getItem, setItem } = useLocalStorage();
  const [hashboardSerials, setHashboardSerials] = useState<string[]>();
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
    <>
      {noPoolsLive && (
        <NoPoolsCallout arePoolsConfigured={!!poolsInfo?.[0]?.url} />
      )}

      <div className="flex flex-col mb-4">
        <div className="flex items-center pb-6">
          <div className="text-heading-300 grow">Home</div>
          <DurationSelector
            className="h-fit"
            duration={duration}
            onSelect={setDuration}
          />
        </div>

        <div className="w-[calc(100%+12*var(--spacing))] -ml-6 pb-11 phone:w-full phone:ml-0 phone:pb-6">
          <TabMenu
            hashrate={minerHashrate?.aggregates.avg}
            efficiency={minerEfficiency?.aggregates.avg}
            powerUsage={minerPowerUsage?.aggregates.avg}
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
          <div className="flex h-full items-center justify-center">
            <Spinner />
          </div>
        )}
      </div>
    </>
  );
};

export default KpiLayout;
