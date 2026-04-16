import { useCallback } from "react";
import clsx from "clsx";
import { type TemperatureUnit } from "./types";

import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";

interface TemperatureUnitsSwitcherProps {
  onClickDone: () => void;
  temperatureUnit: TemperatureUnit;
  setTemperatureUnit: (unit: TemperatureUnit) => void;
}

const TemperatureUnitsSwitcher = ({
  onClickDone,
  temperatureUnit,
  setTemperatureUnit,
}: TemperatureUnitsSwitcherProps) => {
  const handleChange = useCallback(
    (id: string, isSelected: boolean) => {
      const unit = id as TemperatureUnit;
      if (isSelected) {
        setTemperatureUnit(unit);
      }
    },
    [setTemperatureUnit],
  );

  return (
    <Modal
      title="Temperature"
      onDismiss={onClickDone}
      buttons={[
        {
          text: "Done",
          onClick: onClickDone,
          variant: variants.secondary,
        },
      ]}
      divider={false}
    >
      <div className="mt-6 flex flex-col gap-4">
        <SelectRow
          id={"C"}
          text="Celsius (ºC)"
          isSelected={temperatureUnit === "C"}
          onChange={handleChange}
          divider={false}
          className={clsx("border-1 border-border-5", {
            "border-border-20": temperatureUnit === "C",
          })}
          type={selectTypes.radio}
          data-testid="celsius-option"
        />
        <SelectRow
          id={"F"}
          text="Fahrenheit (ºF)"
          isSelected={temperatureUnit === "F"}
          onChange={handleChange}
          divider={false}
          className={clsx("border-1 border-border-5", {
            "border-border-20": temperatureUnit === "F",
          })}
          type={selectTypes.radio}
          data-testid="fahrenheit-option"
        />
      </div>
    </Modal>
  );
};

export default TemperatureUnitsSwitcher;
