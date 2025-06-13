import { useCallback } from "react";

import { TEMP_UNITS } from "./constants";
import usePreferences from "./hooks/usePreferences";
import { TemperatureUnits } from "./types";

import { variants } from "@/shared/components/Button";
import PageOverlay from "@/shared/components/PageOverlay";
import { popoverSizes } from "@/shared/components/Popover";
import PopoverContent from "@/shared/components/Popover/PopoverContent.tsx";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";

interface TemperatureUnitsSwitcherProps {
  onClickDone: () => void;
}

const TemperatureUnitsSwitcher = ({
  onClickDone,
}: TemperatureUnitsSwitcherProps) => {
  const { temperatureUnits, setTemperatureUnits } = usePreferences();

  const handleChange = useCallback(
    (id: string, isSelected: boolean) => {
      const units = id as TemperatureUnits;
      if (isSelected) {
        setTemperatureUnits(units);
      }
    },
    [setTemperatureUnits],
  );

  // TODO should be modal instead of Popover
  return (
    <PageOverlay show>
      <PopoverContent
        closePopover={onClickDone}
        title="Tempeature"
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
            id={TEMP_UNITS.celsius}
            text="Celsius (ºC)"
            isSelected={temperatureUnits === TEMP_UNITS.celsius}
            onChange={handleChange}
            type={selectTypes.radio}
          />
          <SelectRow
            id={TEMP_UNITS.fahrenheit}
            text="Fahrenheit (ºF)"
            isSelected={temperatureUnits === TEMP_UNITS.fahrenheit}
            onChange={handleChange}
            type={selectTypes.radio}
          />
        </div>
      </PopoverContent>
    </PageOverlay>
  );
};

export default TemperatureUnitsSwitcher;
