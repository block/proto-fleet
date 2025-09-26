import { action } from "storybook/actions";

import TemperatureUnitsSwitcherComponent from "./TemperatureUnitsSwitcher";

export const TemperatureUnitsSwitcher = () => {
  return (
    <TemperatureUnitsSwitcherComponent onClickDone={action("Done clicked")} />
  );
};

export default {
  title: "Components (Shared)/Temperature Units Switcher",
};
