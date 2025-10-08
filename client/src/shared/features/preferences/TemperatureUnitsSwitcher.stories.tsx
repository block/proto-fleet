import { action } from "storybook/actions";

import TemperatureUnitsSwitcherComponent from "./TemperatureUnitsSwitcher";

export const TemperatureUnitsSwitcher = () => {
  return (
    <TemperatureUnitsSwitcherComponent onClickDone={action("Done clicked")} />
  );
};

export default {
  title: "Shared/Temperature Units Switcher",
};
