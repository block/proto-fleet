import { useCallback } from "react";
import { type TemperatureUnit } from "./types";

import { variants } from "@/shared/components/Button";
import PageOverlay from "@/shared/components/PageOverlay";
import { popoverSizes } from "@/shared/components/Popover";
import PopoverContent from "@/shared/components/Popover/PopoverContent.tsx";
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

  // TODO should be modal instead of Popover
  return (
    <PageOverlay show>
      <PopoverContent
        closePopover={onClickDone}
        title="Temperature"
        buttons={[
          {
            text: "Done",
            onClick: onClickDone,
            variant: variants.secondary,
          },
        ]}
        titleSize="text-heading-100"
        size={popoverSizes.medium}
      >
        <div className="-mt-3">
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
        </div>
      </PopoverContent>
    </PageOverlay>
  );
};

export default TemperatureUnitsSwitcher;
