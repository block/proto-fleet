import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import AsicTable from "./Asic/AsicTableWrapper";
import HashboardSelector from "./HashboardSelector";
import { useHashboardStatus, useTimeSeries } from "@/protoOS/api";
import { AsicFieldType, HashboardFieldType } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  convertAndFormatMeasurement,
  convertValueUnits,
  formatValue,
  getCurrentValue,
  HashboardData,
  type Measurement,
  useDuration,
  useMinerHashboard,
  useMinerHashboards,
} from "@/protoOS/store";
import { Dismiss } from "@/shared/assets/icons";
import Header from "@/shared/components/Header";
import { PopoverProvider } from "@/shared/components/Popover";
import Stats, { type StatsProps } from "@/shared/components/Stats";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences";

const getStats = (
  avgAsicTemp: Measurement | undefined,
  maxAsicTemp: Measurement | undefined,
  powerUsage: Measurement | undefined,
  hashrate: Measurement | undefined,
): StatsProps["stats"] => {
  return [
    {
      label: "Highest ASIC temp",
      value: formatValue(maxAsicTemp),
      units: maxAsicTemp?.units,
    },
    {
      label: "Avg ASIC temp",
      value: formatValue(avgAsicTemp),
      units: avgAsicTemp?.units,
    },
    {
      label: "Board power usage",
      value: powerUsage?.formatted,
      units: powerUsage?.units,
    },
    {
      label: "Board hashrate",
      value: hashrate?.formatted,
      units: hashrate?.units,
    },
  ];
};

const containerPadX = "px-14 tablet:px-10 phone:px-6";
const containerMarginX = "mx-14 tablet:mx-10 phone:mx-6";

type HashboardTemperatureProps = {
  serial: string;
};

const HashboardTemperature = ({ serial }: HashboardTemperatureProps) => {
  const { temperatureUnits } = usePreferences();
  const isFahrenheit = temperatureUnits === TEMP_UNITS.fahrenheit;
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);
  const { minerRoot } = useMinerHosting();
  const duration = useDuration();

  const navigate = useNavigate();

  // TODO: [STORE_REFACTOR] Eventually we should be able to remove this api call
  // currently it fills in gaps between our useHardware call and useTimeSeries calls
  // - asic rows and columns are missing from useHardware
  // - inlet/outlet temps and avg/max asic temps are missing from useTimeSeries
  useHashboardStatus({
    hashboardSerialNumber: serial,
  });

  // Memoize levels to prevent recreating on every render
  const levels = useMemo(
    () => [
      {
        type: "hashboard" as const,
        fields: [
          HashboardFieldType.Temperature,
          HashboardFieldType.Power,
          HashboardFieldType.Hashrate,
        ],
      },
      {
        type: "asic" as const,
        fields: [AsicFieldType.Temperature, AsicFieldType.Hashrate],
      },
    ],
    [],
  );

  // Fetch telemetry data with polling
  useTimeSeries({
    duration,
    levels,
    poll: true,
    pollIntervalMs: 10000,
  });

  const close = () => {
    navigate(minerRoot + `/temperature`);
  };

  // Get hashboard data from store
  const hashboard = useMinerHashboard(serial);
  const hashboards = useMinerHashboards();
  const hashboardList = useMemo(() => {
    return hashboards
      .filter((h): h is HashboardData & { slot: number } => !!h.slot)
      .sort((a, b) => a.slot - b.slot)
      .map((hashboard) => ({
        serial: hashboard.serial,
        name: `Hashboard ${hashboard.slot}`,
      }));
  }, [hashboards]);

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
              convertValueUnits(
                hashboard?.avgAsicTemp,
                isFahrenheit ? "F" : "C",
              ),
              convertValueUnits(
                hashboard?.maxAsicTemp,
                isFahrenheit ? "F" : "C",
              ),
              getCurrentValue(hashboard?.power, "kW", false),
              getCurrentValue(hashboard?.hashrate, "TH/S", false),
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
              {hashboard?.inletTemp && (
                <>
                  {" "}
                  {convertAndFormatMeasurement(
                    hashboard.inletTemp,
                    isFahrenheit ? "F" : "C",
                    false,
                  )}
                </>
              )}
            </div>
            <div className="relative bg-surface-base px-4">{serial}</div>
            <div className="relative bg-surface-base pl-4">
              Rear
              {hashboard?.outletTemp && (
                <>
                  {" "}
                  {convertAndFormatMeasurement(
                    hashboard.outletTemp,
                    isFahrenheit ? "F" : "C",
                    false,
                  )}
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
              hashboardSerialNumber={serial}
              showPopover={showPopover}
              setShowPopover={setShowPopover}
            />
          </div>
        </div>
      )}
      <div className={`${containerPadX} mb-5`}>
        {hashboard?.board && (
          <div className="before:w-ful relative flex items-center justify-around font-mono text-mono-text-50 text-text-primary-50 before:absolute before:top-[50%] before:left-0 before:h-[1px] before:w-full before:bg-border-5">
            <div className="relative bg-surface-base px-4">
              {hashboard.board}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default HashboardTemperature;
