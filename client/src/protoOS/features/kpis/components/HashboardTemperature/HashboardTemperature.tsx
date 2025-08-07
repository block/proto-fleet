import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import AsicTable from "./Asic/AsicTableWrapper";
import HashboardSelector from "./HashboardSelector";
import {
  useHashboards,
  useHashboardStats,
  useHashboardTemperature,
  useMinerHosting,
} from "@/protoOS/api";
import { Aggregates, HashboardStatsHashboardstats } from "@/protoOS/api/types";
import { useGranularity } from "@/protoOS/features/kpis/hooks";
import useHashboardLocationStore from "@/protoOS/store/useHashboardLocationStore";
import { Dismiss } from "@/shared/assets/icons";
import { Duration } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import { PopoverProvider } from "@/shared/components/Popover";
import Stats, {
  type StatsProps,
} from "@/shared/features/kpis/components/Stats";
import {
  TEMP_UNITS,
  TemperatureUnits,
  usePreferences,
} from "@/shared/features/preferences";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";
import {
  convertCtoF,
  convertWtoKW,
  getAsicTempValue,
} from "@/shared/utils/utility";

const getStats = (
  avgHashboardTemp: Aggregates["avg"],
  avgAsicTemp: HashboardStatsHashboardstats["avg_asic_temp_c"],
  powerUsage: HashboardStatsHashboardstats["power_usage_watts"],
  units: TemperatureUnits,
): StatsProps["stats"] => {
  const isFahrenheit = units === TEMP_UNITS.fahrenheit;
  const unit = isFahrenheit ? "ºF" : "ºC";

  return [
    {
      label: "Hashboard avg. temp",
      value:
        isFahrenheit && avgHashboardTemp
          ? convertCtoF(avgHashboardTemp)
          : avgHashboardTemp,
      units: unit,
    },
    {
      label: "Current avg. ASIC temp",
      value: getAsicTempValue(avgAsicTemp, isFahrenheit),
      units: avgAsicTemp ? unit : undefined,
    },
    {
      label: "Power usage",
      value: powerUsage ? convertWtoKW(powerUsage) : undefined,
      units: "kW",
    },
  ];
};

const containerPadX = "px-14 tablet:px-10 phone:px-6";
const containerMarginX = "mx-14 tablet:mx-10 phone:mx-6";

type HashboardTemperatureProps = {
  serial: string;
};

const HashboardTemperature = ({ serial }: HashboardTemperatureProps) => {
  const { getItem } = useLocalStorage();
  const { temperatureUnits } = usePreferences();
  const [avgAsicTemp, setAvgAsicTemp] = useState<number | undefined>(undefined);
  const [powerUsage, setPowerUsage] = useState<number | undefined>(undefined);
  const [avgHashboardTemp, setAvgHashboardTemp] = useState<number | undefined>(
    undefined,
  );
  const getSlotByHbSn = useHashboardLocationStore(
    (state) => state.getSlotByHbSn,
  );

  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const { minerRoot } = useMinerHosting();
  const [hashboardList, setHashboardList] = useState<
    { serial: string; name: string }[]
  >([]);

  const navigate = useNavigate();
  const granularity = useGranularity();

  const close = () => {
    navigate(minerRoot + `/temperature`);
  };

  // TODO: Data that doesnt change often like hashboard serials
  // should be cached in the context, data store or local storage
  // [DASH-228]
  const { data: hashboardsInfo } = useHashboards();
  useEffect(() => {
    if (hashboardsInfo) {
      setHashboardList(
        hashboardsInfo
          .filter((hashboardInfo) => hashboardInfo.hb_sn !== undefined)
          .sort(
            (a, b) =>
              (getSlotByHbSn(a.hb_sn as string) ?? Number.MAX_SAFE_INTEGER) -
              (getSlotByHbSn(b.hb_sn as string) ?? Number.MAX_SAFE_INTEGER),
          )
          .map((hashboardInfo) => ({
            serial: hashboardInfo.hb_sn as string,
            name: "Hashboard " + getSlotByHbSn(hashboardInfo.hb_sn as string),
          })),
      );
    }
  }, [hashboardsInfo, getSlotByHbSn]);

  // TODO: We need to add a cacheing strategy where we can show stale data thats less
  // than a predermined ttl while we wait for the next poll to come in.
  // [DASH-228]
  const duration = getItem("duration") as Duration;
  const hbTemperature = useHashboardTemperature({
    hashboardSerial: serial,
    duration: duration,
    poll: true,
  });

  // fetch hashboard stats for average asic temp and power usage
  const hbStats = useHashboardStats({
    hashboardSerialNumber: serial,
    poll: true,
  });

  // Updates average hashboard temperature
  // unset the state when the serial changes showing the loading state
  useEffect(() => {
    if (
      !hbTemperature?.data ||
      hbTemperature?.data?.hashboardSerial !== serial
    ) {
      setAvgHashboardTemp(undefined);
      return;
    }

    const aggregates = hbTemperature.data?.aggregates as Aggregates;
    setAvgHashboardTemp(aggregates?.avg);
  }, [hbTemperature, serial]);

  // Updates average asic temp and power usage
  // unset the state when the serial changes showing the loading state
  useEffect(() => {
    if (!hbStats?.data || hbStats?.data.hb_sn !== serial) {
      setAvgAsicTemp(undefined);
      setPowerUsage(undefined);
      return;
    }

    setAvgAsicTemp(hbStats?.data?.avg_asic_temp_c);
    setPowerUsage(hbStats?.data?.power_usage_watts);
  }, [hbStats, serial]);

  return (
    <div className="min-h-[100vh] w-full bg-surface-base">
      <Header
        className="fixed z-10 h-16 items-center border-b border-border-5 bg-surface-base px-4"
        centerButton={true}
        icon={<Dismiss width="w-3.5" />}
        iconVariant="textOnly"
        iconTextColor="text-text-primary"
        iconOnClick={close}
        inline={true}
        title="Hashboards"
        titleSize="text-heading-200"
        buttons={[
          {
            text: "Done",
            variant: "primary",
            onClick: close,
          },
        ]}
      />
      <div className={`pt-24 pb-8 ${containerPadX}`}>
        <PopoverProvider>
          <HashboardSelector
            hashboardList={hashboardList}
            currentHashboard={serial}
          />
        </PopoverProvider>
      </div>
      <div className="max-w-screen overflow-visible overflow-x-auto">
        <div className={`${containerPadX} phone:mx-6 phone:!px-0`}>
          <Stats
            stats={getStats(
              avgHashboardTemp,
              avgAsicTemp,
              powerUsage,
              temperatureUnits,
            )}
            size="medium"
            gap="gap-10"
            padding="pb-4"
          />
        </div>
      </div>
      <div className={`${containerPadX} pt-4`}>
        {serial && (
          <div className="before:w-ful relative flex items-center justify-between font-mono text-mono-text-50 text-text-primary-50 before:absolute before:top-[50%] before:left-0 before:h-[1px] before:w-full before:bg-border-5">
            <div className="relative bg-surface-base pr-4">Front</div>
            <div className="relative bg-surface-base px-4">{serial}</div>
            <div className="relative bg-surface-base pl-4">Rear</div>
          </div>
        )}
      </div>
      {serial && (
        <div className="max-w-screen overflow-visible overflow-x-auto">
          <div className={`relative ${containerMarginX} mb-5 min-w-[450px]`}>
            <AsicTable
              duration={duration}
              granularity={granularity}
              hashboardSerialNumber={serial}
              showPopover={showPopover}
              setShowPopover={setShowPopover}
            />
          </div>
        </div>
      )}
    </div>
  );
};

export default HashboardTemperature;
