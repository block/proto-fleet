import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useShallow } from "zustand/shallow";
import AsicTable from "./Asic/AsicTableWrapper";
import HashboardSelector from "./HashboardSelector";
import { useHashboards, useMinerHosting } from "@/protoOS/api";
import { HashboardStatsHashboardstats } from "@/protoOS/api/types";
import { useGranularity } from "@/protoOS/features/kpis/hooks";
import useHashboardAsicStore from "@/protoOS/store/useHashboardAsicStore";
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
import { getDisplayValue } from "@/shared/utils/stringUtils";
import {
  convertCtoF,
  convertGigahashSecToTerahashSec,
  convertWtoKW,
  getAsicTempValue,
} from "@/shared/utils/utility";

const getStats = (
  avgAsicTemp: HashboardStatsHashboardstats["avg_asic_temp_c"],
  maxAsicTemp: number | undefined,
  powerUsage: HashboardStatsHashboardstats["power_usage_watts"],
  hashrateGhs: HashboardStatsHashboardstats["hashrate_ghs"],
  temperatureUnits: TemperatureUnits,
): StatsProps["stats"] => {
  const isFahrenheit = temperatureUnits === TEMP_UNITS.fahrenheit;
  const unit = isFahrenheit ? "ºF" : "ºC";

  return [
    {
      label: "Highest ASIC temp",
      value: getAsicTempValue(maxAsicTemp, isFahrenheit),
      units: maxAsicTemp ? unit : undefined,
    },
    {
      label: "Avg ASIC temp",
      value: getAsicTempValue(avgAsicTemp, isFahrenheit),
      units: avgAsicTemp ? unit : undefined,
    },
    {
      label: "Board power usage",
      value: powerUsage ? convertWtoKW(powerUsage) : undefined,
      units: "kW",
    },
    {
      label: "Board hashrate",
      value: hashrateGhs
        ? convertGigahashSecToTerahashSec(hashrateGhs)
        : undefined,
      units: "TH/S",
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
  const isFahrenheit = temperatureUnits === TEMP_UNITS.fahrenheit;
  const getSlotByHbSn = useHashboardLocationStore(
    (state) => state.getSlotByHbSn,
  );
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const { minerRoot } = useMinerHosting();
  const [hashboardList, setHashboardList] = useState<
    { serial: string; name: string }[]
  >([]);

  const duration = getItem("duration") as Duration;

  const navigate = useNavigate();
  const granularity = useGranularity();

  const close = () => {
    navigate(minerRoot + `/temperature`);
  };

  const {
    avgAsicTempC,
    maxAsicTempC,
    powerUsageWatts,
    inletTempC,
    outletTempC,
    hashrateGhs,
  } = useHashboardAsicStore(
    useShallow((state) => {
      return {
        avgAsicTempC: state.hashboards.get(serial)?.avgAsicTempC,
        maxAsicTempC: state.getMaxCurrentAsicTemp(serial),
        powerUsageWatts: state.hashboards.get(serial)?.powerUsageWatts,
        inletTempC: state.hashboards.get(serial)?.inletTempC,
        outletTempC: state.hashboards.get(serial)?.outletTempC,
        hashrateGhs: state.hashboards.get(serial)?.hashrateGhs,
      };
    }),
  );

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

  // TODO: This is the lightest touch way to include model with current data pipeline,
  // this is all refactored in mflesher/dash-671-part-2 so I wanted to keep rebase conflicts minimal
  const model = useMemo(() => {
    if (!hashboardsInfo) return;
    const hashboard = hashboardsInfo.find((hboard) => hboard.hb_sn === serial);
    return hashboard?.board;
  }, [hashboardsInfo, serial]);

  // Updates average asic temp and power usage
  // unset the state when the serial changes showing the loading state
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
              avgAsicTempC,
              maxAsicTempC,
              powerUsageWatts,
              hashrateGhs,
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
            <div className="relative bg-surface-base pr-4">
              Front
              {inletTempC && (
                <>
                  {isFahrenheit
                    ? ` ${getDisplayValue(convertCtoF(inletTempC))}º`
                    : ` ${getDisplayValue(inletTempC)}º`}
                </>
              )}
            </div>
            <div className="relative bg-surface-base px-4">{serial}</div>
            <div className="relative bg-surface-base pl-4">
              Rear
              {outletTempC && (
                <>
                  {isFahrenheit
                    ? ` ${getDisplayValue(convertCtoF(outletTempC))}º`
                    : ` ${getDisplayValue(outletTempC)}º`}
                </>
              )}
            </div>
          </div>
        )}
      </div>
      {serial && (
        <div className="scrollbar-hide max-w-screen overflow-x-auto">
          <div className={`relative ${containerMarginX} mb-2 min-w-[800px]`}>
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
      <div className={`${containerPadX} mb-5`}>
        {model && (
          <div className="before:w-ful relative flex items-center justify-around font-mono text-mono-text-50 text-text-primary-50 before:absolute before:top-[50%] before:left-0 before:h-[1px] before:w-full before:bg-border-5">
            <div className="relative bg-surface-base px-4">{model}</div>
          </div>
        )}
      </div>
    </div>
  );
};

export default HashboardTemperature;
