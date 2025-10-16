import { ReactNode, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";
import AsicTablePreview from "./AsicTablePreview";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { criticalTemp } from "@/protoOS/features/kpis/constants";
import {
  convertAndFormatMeasurement,
  useMinerHashboard,
  useTemperatureUnit,
} from "@/protoOS/store";
import SkeletonBar from "@/shared/components/SkeletonBar";

type HbTempPreviewProps = {
  serial: string | null;
  slot: number;
};

// TODO: [STRORE_REFACTOR] We should add an xsmall variant of share/componentes/Stat and use here
const TempDisplay = ({
  formattedTemp,
  label,
}: {
  formattedTemp: string | undefined;
  label: string;
}) => {
  return (
    <div className="flex flex-col">
      <div className="text-emphasis-300 text-text-primary-70">
        {formattedTemp ? formattedTemp : <SkeletonBar className="my-1" />}
      </div>
      <div className="text-xs text-text-primary-50">{label}</div>
    </div>
  );
};

const WrapperComponent = ({
  serial,
  children,
  isOverheating,
}: {
  serial: string | null;
  children: ReactNode;
  isOverheating: boolean;
}) => {
  const { minerRoot } = useMinerHosting();
  const sharedClassName =
    "group block overflow-hidden phone:w-full phone:rounded-xl phone:border-1 phone:border-border-10";
  return (
    <>
      {serial ? (
        <Link
          data-testid="hb-temp-preview"
          to={`${minerRoot}/temperature/${serial}`}
          className={clsx(
            sharedClassName,
            isOverheating
              ? "hover:bg-intent-critical-20"
              : "hover:bg-core-primary-2",
          )}
        >
          {children}
        </Link>
      ) : (
        <div className={sharedClassName}>{children}</div>
      )}
    </>
  );
};

const HbTempPreview = ({ serial, slot }: HbTempPreviewProps) => {
  const [isOverheating, setIsOverheating] = useState<boolean>(false);

  const temperatureUnit = useTemperatureUnit();

  const hashboard = useMinerHashboard(serial);

  useEffect(() => {
    if (!hashboard || !hashboard.temperature?.latest) return;

    const lastTemp = hashboard.temperature.latest.value;
    setIsOverheating(!!lastTemp && lastTemp > criticalTemp);
  }, [hashboard, temperatureUnit]);

  return (
    <WrapperComponent serial={serial} isOverheating={isOverheating}>
      <div
        className={clsx(
          "relative flex justify-between px-4 py-1",
          isOverheating
            ? "bg-intent-critical-20 group-hover:bg-transparent"
            : "bg-core-primary-5",
        )}
      >
        <h3
          className={clsx(
            "text-emphasis-300",
            isOverheating
              ? "text-intent-critical-text"
              : "text-text-primary-70",
          )}
        >
          Hashboard {slot}
        </h3>
      </div>

      {hashboard && serial ? (
        <>
          <div className="px-4 py-2">
            <div className="grid grid-cols-2 gap-x-4">
              <TempDisplay
                formattedTemp={convertAndFormatMeasurement(
                  hashboard?.avgAsicTemp?.latest,
                  temperatureUnit,
                  true,
                )}
                label="ASIC avg"
              />

              <TempDisplay
                formattedTemp={convertAndFormatMeasurement(
                  hashboard?.maxAsicTemp?.latest,
                  temperatureUnit,
                  true,
                )}
                label="ASIC high"
              />
            </div>
          </div>

          <div className="p-4">
            <AsicTablePreview hashboardSerial={serial} />
          </div>
        </>
      ) : (
        <div className="flex h-41 items-center justify-center text-text-primary-70">
          No Hashboard
        </div>
      )}
    </WrapperComponent>
  );
};

export default HbTempPreview;
