import { useEffect, useMemo, useState } from "react";
import { Outlet } from "react-router-dom";
import {
  useProcessedEfficiency,
  useProcessedHashrate,
  useProcessedPowerUsage,
  useProcessedTemperature,
} from "../../hooks";
import { type OutletContext } from "../../types";
import NoPoolsCallout from "../NoPoolsCallout";
import { useHashboards } from "@/protoOS/api";
import { useMinerStatus } from "@/protoOS/contexts/MinerStatusContext";
import TabMenu from "@/protoOS/features/kpis/components/TabMenu";
import DurationSelector, {
  Duration,
  durations,
} from "@/shared/components/DurationSelector";
import Spinner from "@/shared/components/Spinner";
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
    <div className="p-14 phone:p-6 tablet:p-10">
      {noPoolsLive && (
        <NoPoolsCallout arePoolsConfigured={!!poolsInfo?.[0]?.url} />
      )}

      <div className="mb-4 flex flex-col">
        <div className="flex items-center pb-6">
          <div className="grow text-heading-300">Home</div>
          <DurationSelector
            className="h-fit"
            duration={duration}
            onSelect={setDuration}
          />
        </div>

        <div className="-ml-6 w-[calc(100%+12*var(--spacing))] pb-11 phone:ml-0 phone:w-full phone:pb-6">
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
    </div>
  );
};

export default KpiLayout;
