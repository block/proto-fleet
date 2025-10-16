import clsx from "clsx";
import HbBayPreview from "./HbBayPreview";
import { useCoolingStatus, useTelemetry } from "@/protoOS/api";
import {
  type FanTelemetryData,
  formatValue,
  useFansTelemetry,
  useHashboardSerialsByBay,
} from "@/protoOS/store";
import { FanIndicator } from "@/shared/assets/icons";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

const getFanStats = (
  fanData: FanTelemetryData | undefined,
  numFans: number,
  fanIndex: number,
  isR1?: boolean,
) => {
  if (!fanData?.rpm?.latest || !fanData?.percentage?.latest) return null;

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
    value: formatValue(fanData.percentage.latest, false),
    text: formatValue(fanData.rpm.latest, true),
    units: "%",
    icon: <FanIndicator {...fanProps} />,
  } as StatProps;
};

const Temperature = () => {
  // Fetch latest telemetry data with polling
  // this fetches miner, hashboard, and asic level data
  useTelemetry({
    level: ["asic"],
  });

  // Fetch fan telemetry which is not yet included in Telemetry API
  useCoolingStatus({ poll: true });

  // fetch hashboard serials organized by bay
  const hashboardSerialsByBay = useHashboardSerialsByBay();
  const fans = useFansTelemetry(); // fan telemetry data

  return (
    <div className="flex flex-col gap-y-8 pt-4">
      {fans.length > 0 && (
        <Stats
          size="medium"
          grid={clsx(
            fans.length < 6
              ? "grid-cols-4 phone:grid-cols-2"
              : "grid-cols-6 laptop:grid-cols-3 laptop:grid-rows-2 tablet:grid-cols-3 tablet:grid-rows-2 phone:grid-cols-2 phone:grid-rows-3 grid-flow-col phone:grid-flow-row",
          )}
          // use padding and negative margin instead of gap-x to create even spacing around divider
          gap={clsx(
            "gap-y-6",
            fans.length < 6
              ? "*:px-10 -mx-10 phone:*:px-6 phone:-mx-6"
              : "*:px-10 -mx-10 desktop:*:px-5 desktop:-mx-5 phone:*:px-6 phone:-mx-6",
          )}
          padding="pb-0"
          divide="*:border-r *:border-border-5 *:last:border-0 laptop:*:nth-last-2:border-0 tablet:*:nth-last-2:border-0  phone:*:even:border-0"
          stats={fans
            .map((fanData, index) =>
              getFanStats(fanData, fans.length, index, fans.length === 4),
            )
            .filter((stat) => stat !== null)}
        />
      )}

      <div className="flex grid-cols-3 flex-col gap-4 sm:grid">
        {Object.values(hashboardSerialsByBay).map((serials, bayIndex) => {
          return (
            <div key={bayIndex}>
              <HbBayPreview serials={serials} bay={bayIndex} />
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default Temperature;
