import { action } from "@storybook/addon-actions";
import CoolingComponent from "./Cooling";

export const Cooling = () => {
  return <CoolingComponent onChange={(fanMode) => action("fan mode")(fanMode)} />;
};

export default {
  title: "Components/Cooling",
};
