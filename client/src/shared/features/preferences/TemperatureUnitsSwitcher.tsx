import { useCallback } from "react";
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
      size="small"
    >
      <SelectRow
        id={"C"}
        text="Celsius (ºC)"
        isSelected={temperatureUnit === "C"}
        onChange={handleChange}
        type={selectTypes.radio}
        data-testid="celsius-option"
      />
      <SelectRow
        id={"F"}
        text="Fahrenheit (ºF)"
        isSelected={temperatureUnit === "F"}
        onChange={handleChange}
        type={selectTypes.radio}
        data-testid="fahrenheit-option"
      />
    </Modal>
  );
};

export default TemperatureUnitsSwitcher;
