import { useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { useShallow } from "zustand/react/shallow";
import HbBayPreview from "./HbBayPreview";
import { useCoolingStatus, useTelemetry } from "@/protoOS/api";
import { type FanStatus } from "@/protoOS/api/generatedApi";
import { useMinerHashboards, useMinerStore } from "@/protoOS/store";
import { FanIndicator } from "@/shared/assets/icons";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

const getFanStats = (
  fanSpeed: FanStatus | null | undefined,
  numFans: number,
  fanIndex: number,
  isR1?: boolean,
) => {
  if (!fanSpeed) return null;

  const fanPosition = fanIndex + 1;
  let label = `Fan ${fanPosition}`;
  let fanProps = { numFans, fanPosition };

  // For R1 models, we need to adjust the label and indicator to
  // display Front and Rear fans separately
  if (isR1) {
    const position = Math.floor(fanIndex / 2) < 1 ? "Front" : "Rear";
    const slot = (fanIndex % 2) + 1;
    label = `${position} Fan ${slot} Speed`;

    fanProps = {
      numFans: 2,
      fanPosition: fanIndex % 2,
    };
  }

  return {
    label: label,
    value: fanSpeed.percentage,
    text: `${fanSpeed.rpm} RPM`,
    units: "%",
    icon: <FanIndicator {...fanProps} />,
  } as StatProps;
};

const Temperature = () => {
  const [fanSpeeds, setFanSpeeds] = useState<(FanStatus | null)[]>();
  const bayCount = useMinerStore(
    useShallow((state) => state.hardware.getBayCount()),
  );

  // Fetch latest telemetry data with polling
  useTelemetry({
    level: "asic",
  });

  // Get integrated hashboard data directly from stores
  // Only fetch after hashboards are loaded
  const hashboards = useMinerHashboards();

  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus({ poll: true });

  // Organize hashboards by bay to avoid filtering on every render
  const hashboardsByBay = useMemo(() => {
    const byBay: { [bay: number]: typeof hashboards } = {};
    hashboards.forEach((hashboard) => {
      if (!hashboard.bay) return;
      if (!byBay[hashboard.bay]) {
        byBay[hashboard.bay] = [];
      }
      byBay[hashboard.bay].push(hashboard);
    });
    return byBay;
  }, [hashboards]);

  useEffect(() => {
    if (!pendingCoolingStatus || coolingStatus?.fans) {
      setFanSpeeds(coolingStatus?.fans);
    }
  }, [coolingStatus, pendingCoolingStatus]);

  return (
    <div className="flex flex-col gap-y-8 pt-4">
      {fanSpeeds && (
        <Stats
          size="medium"
          grid={clsx(
            fanSpeeds.length < 6
              ? "grid-cols-4 phone:grid-cols-2"
              : "grid-cols-6 laptop:grid-cols-3 laptop:grid-rows-2 tablet:grid-cols-3 tablet:grid-rows-2 phone:grid-cols-2 phone:grid-rows-3 grid-flow-col phone:grid-flow-row",
          )}
          // use padding and negative margin instead of gap-x to create even spacing around divider
          gap={clsx(
            "gap-y-6",
            fanSpeeds.length < 6
              ? "*:px-10 -mx-10 phone:*:px-6 phone:-mx-6"
              : "*:px-10 -mx-10 desktop:*:px-5 desktop:-mx-5 phone:*:px-6 phone:-mx-6",
          )}
          padding="pb-0"
          divide="*:border-r *:border-border-5 *:last:border-0 laptop:*:nth-last-2:border-0 tablet:*:nth-last-2:border-0  phone:*:even:border-0"
          stats={fanSpeeds
            .map((fanSpeed, index) =>
              getFanStats(
                fanSpeed,
                fanSpeeds.length,
                index,
                fanSpeeds.length === 4,
              ),
            )
            .filter((stat) => stat !== null)}
        />
      )}

      <div className="flex grid-cols-3 flex-col gap-4 sm:grid">
        {Array.from({ length: bayCount }).map((_, bayIndex) => {
          const bayNumber = bayIndex + 1;
          const bayData = hashboardsByBay[bayNumber] || [];

          return (
            <div key={bayIndex}>
              <HbBayPreview data={bayData} />
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default Temperature;
