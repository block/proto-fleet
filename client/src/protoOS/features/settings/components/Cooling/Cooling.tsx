import React, { useCallback, useEffect, useState } from "react";
import clsx from "clsx";
import { useCoolingStatus } from "@/protoOS/api";
import { CoolingConfig } from "@/protoOS/api/types";
import { Fan } from "@/shared/assets/icons";
import Immersion from "@/shared/assets/icons/Immersion";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";
import { pushToast, updateToast } from "@/shared/features/toaster";

interface CoolingOptionProps {
  title: string;
  description: string;
  icon?: React.ReactNode;
  isSelected?: boolean;
}

const CoolingOption = ({
  title,
  description,
  icon,
  isSelected = false,
}: CoolingOptionProps) => (
  <div className="flex items-center justify-start gap-4">
    {icon ? (
      <div
        className={clsx("flex h-8 w-8 items-center justify-center rounded-lg", {
          "bg-core-primary-5": isSelected,
          "bg-surface-5": !isSelected,
        })}
      >
        {icon}
      </div>
    ) : null}
    <div>
      <h4 className="text-emphasis-300">{title}</h4>
      <p className="text-200 text-text-primary-70">{description}</p>
    </div>
  </div>
);

const COOLING_MODES = {
  air: "air-cooled",
  immersion: "immersion-cooled",
} as const;

type CoolingMode = (typeof COOLING_MODES)[keyof typeof COOLING_MODES];

const FAN_MODES: {
  [K in CoolingMode]: string;
} = {
  [COOLING_MODES.air]: "Auto",
  [COOLING_MODES.immersion]: "Off",
} as const;

const disabledClassName = "opacity-50 pointer-events-none";

const isSelected = (
  coolingMode: CoolingMode | undefined,
  userSelectedCoolingMode: CoolingMode | undefined,
  pending: boolean,
  expected: CoolingMode,
) => {
  if (!coolingMode) return false;
  if (pending && userSelectedCoolingMode === expected) return true;

  return coolingMode === expected && !pending;
};

const Cooling = () => {
  const {
    data: coolingStatus,
    pending,
    setCooling,
  } = useCoolingStatus({ poll: false });
  const [coolingMode, setCoolingMode] = useState<CoolingMode>();
  const [userSelectedCoolingMode, setUserSelectedCoolingMode] =
    useState<CoolingMode>();
  const [loading, setLoading] = useState<boolean>(true);

  useEffect(() => {
    if (coolingStatus) {
      if (coolingStatus.fan_mode === FAN_MODES[COOLING_MODES.air]) {
        setCoolingMode(COOLING_MODES.air);
        setLoading(false);
      } else if (
        coolingStatus.fan_mode === FAN_MODES[COOLING_MODES.immersion]
      ) {
        setCoolingMode(COOLING_MODES.immersion);
        setLoading(false);
      }
    }
  }, [coolingStatus]);

  const handleChange = useCallback(
    (id: string) => {
      setLoading(true);
      setUserSelectedCoolingMode(id as CoolingMode);

      const toast = pushToast({
        message: `Updating cooling mode...`,
        status: "loading",
        ttl: false,
      });

      setCooling({
        mode: FAN_MODES[id as CoolingMode] as CoolingConfig["mode"],
        onSuccess: () => {
          updateToast(toast, {
            message: `Cooling mode updated to ${id.replace("-", " ")}`,
            status: "success",
            ttl: 3000,
          });
        },
        onError: (error) => {
          updateToast(toast, {
            message: `Failed to update cooling mode: ${error?.status}`,
            status: "error",
            ttl: 3000,
          });
          setLoading(false);
          setUserSelectedCoolingMode(undefined);
        },
      });
    },
    [setCooling, setLoading, setUserSelectedCoolingMode],
  );

  return (
    <>
      <h2 className="mb-10 text-heading-300">Cooling</h2>
      <div className="mb-10 flex flex-col gap-4">
        <SelectRow
          id={COOLING_MODES.air}
          isSelected={isSelected(
            coolingMode,
            userSelectedCoolingMode,
            pending,
            COOLING_MODES.air,
          )}
          onChange={handleChange}
          divider={false}
          className={clsx("border-1 border-border-5", {
            "border-border-20": coolingMode === COOLING_MODES.air,
            [disabledClassName]: loading,
          })}
          text={
            <CoolingOption
              title="Air Cooled"
              description="Fans will be used to cool the miner."
              icon={<Fan />}
              isSelected={coolingMode === COOLING_MODES.air}
            />
          }
          type={selectTypes.radio}
        />
        <SelectRow
          id={COOLING_MODES.immersion}
          isSelected={isSelected(
            coolingMode,
            userSelectedCoolingMode,
            pending,
            COOLING_MODES.immersion,
          )}
          onChange={handleChange}
          divider={false}
          className={clsx("border-1 border-border-5", {
            "border-border-20": coolingMode === COOLING_MODES.immersion,
            [disabledClassName]: loading,
          })}
          text={
            <CoolingOption
              title="Immersion Cooled"
              description="Fans must be removed."
              icon={<Immersion />}
              isSelected={coolingMode === COOLING_MODES.immersion}
            />
          }
          type={selectTypes.radio}
        />
      </div>
    </>
  );
};
export default Cooling;
