import { useEffect, useState } from "react";
import { useOutletContext } from "react-router-dom";
import clsx from "clsx";
import { dangerFanspeed, maxFanSpeed, warningFanspeed } from "../../constants";
import { useProcessedHashboardTemperature } from "../../hooks";
import { type OutletContext } from "../../types";
import Stats from "../Stats";
import HbTempPreview from "./HbTempPreview";
import { useCoolingStatus } from "@/protoOS/api";
import { type FanInfo } from "@/protoOS/api/types";
import { FanIndicator } from "@/shared/assets/icons";
import { type StatProps } from "@/shared/components/Stat";
import { map } from "@/shared/utils/math";

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
    value: fanSpeed.rpm,
    units: "RPM",
    icon: <FanIndicator {...fanProps} />,

    // TODO: adjust mapped values when we have actual calibration data
    // for now, we map 85% - 100% maxSpeed to 0 - 100% on chart
    // because otherwide all of the charts appear very close to 100%
    chartPercentage: map(
      fanSpeed.rpm || 0,
      maxFanSpeed * 0.85,
      maxFanSpeed,
      0,
      100,
    ),
    chartStatus:
      fanSpeed.rpm! < dangerFanspeed
        ? "critical"
        : fanSpeed.rpm! < warningFanspeed
          ? "warning"
          : "neutral",
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
      setFanSpeeds(
        coolingStatus?.fans?.concat([
          coolingStatus?.fans[0],
          coolingStatus?.fans[1],
        ]),
      );
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
          gap={clsx(
            "gap-y-6",
            fanSpeeds.length < 6
              ? "gap-x-10 phone:gap-x-6"
              : "gap-x-6 phone:gap-x-6",
          )}
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
      <div className="flex flex-wrap gap-x-6 gap-y-6">
        {hbTempData.map((hbData, index) => (
          <HbTempPreview key={index} hbData={hbData} />
        ))}
      </div>
    </>
  );
};

export default Temperature;
