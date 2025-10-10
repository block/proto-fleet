import { useState } from "react";
import { action } from "storybook/actions";

import TemperatureUnitsSwitcherComponent from "./TemperatureUnitsSwitcher";

export const TemperatureUnitsSwitcher = () => {
  const [temperatureUnit, setTemperatureUnit] = useState<"C" | "F">("C");

  return (
    <TemperatureUnitsSwitcherComponent
      onClickDone={action("Done clicked")}
      temperatureUnit={temperatureUnit}
      setTemperatureUnit={setTemperatureUnit}
    />
  );
};

export default {
  title: "Shared/Temperature Units Switcher",
};
