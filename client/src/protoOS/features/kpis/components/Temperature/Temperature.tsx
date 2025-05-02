import { useEffect, useState } from "react";
import { useOutletContext } from "react-router-dom";
import clsx from "clsx";
import { type OutletContext } from "../../types";
import HbBayPreview from "./HbBayPreview";
import { useCoolingStatus } from "@/protoOS/api";
import { type FanInfo } from "@/protoOS/api/types";
import { useProcessedHashboardTemperature } from "@/protoOS/features/kpis/hooks";
import { FanIndicator } from "@/shared/assets/icons";
import { type StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/features/kpis/components/Stats";

const getFanStats = (
  fanSpeed: FanInfo | undefined,
  numFans: number,
  fanIndex: number,
  isR1?: boolean,
) => {
  if (!fanSpeed) return null;

  let label = `Fan ${fanIndex + 1}`;
  let fanProps = { numFans, fanPosition: fanIndex };

  // For R1 models, we need to adjust the label and indicator to
  // display Front and Rear fans separately
  if (isR1) {
    const position = Math.floor(fanIndex / 2) < 1 ? "Front" : "Rear";
    const slot = (fanIndex % 2) + 1;
    label = `${position} Fan ${slot} Speed`;

    fanProps = {
      numFans: 2,
      fanPosition: position === "Front" ? 1 : 0,
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
  const { duration, hashboardSerials } = useOutletContext<OutletContext>();
  const [fanSpeeds, setFanSpeeds] = useState<FanInfo[]>();

  const hbTempData = useProcessedHashboardTemperature({
    serials: hashboardSerials,
    duration: duration,
  });

  const { data: coolingStatus, pending: pendingCoolingStatus } =
    useCoolingStatus({ poll: true });

  useEffect(() => {
    if (!pendingCoolingStatus || coolingStatus?.fans) {
      setFanSpeeds(coolingStatus?.fans);

      // TODO: Helfpul for faking an R2, but need to rm later
      // setFanSpeeds(
      //   coolingStatus?.fans?.concat([
      //     coolingStatus?.fans[0],
      //     coolingStatus?.fans[1],
      //   ]),
      // );
    }
  }, [coolingStatus, pendingCoolingStatus]);

  return (
    <>
      {fanSpeeds && (
        <Stats
          size="medium"
          grid={clsx(
            fanSpeeds.length < 6
              ? "grid-cols-4 phone:grid-cols-2"
              : "grid-cols-6 tablet:grid-cols-3 phone:grid-cols-2",
          )}
          // use padding and negative margin instead of gap-x to create even spacing around divider
          gap={clsx(
            "gap-y-6",
            fanSpeeds.length < 6
              ? "*:px-10 -mx-10 phone:*:px-6 phone:-mx-6"
              : "*:px-10 -mx-10 phone:*:px-6 phone:-mx-6",
          )}
          padding="pb-4"
          divide="divide-x divide-border-5"
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

      <div className="flex flex-col gap-4">
        {Array.from({ length: Math.ceil(hbTempData.length / 3) }).map(
          (_, groupIndex) => (
            <HbBayPreview
              key={groupIndex}
              data={hbTempData.slice(groupIndex * 3, groupIndex * 3 + 3)}
            />
          ),
        )}
      </div>
    </>
  );
};

export default Temperature;
