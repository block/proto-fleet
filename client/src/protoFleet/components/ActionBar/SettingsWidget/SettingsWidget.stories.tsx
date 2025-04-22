import { useState } from "react";
import clsx from "clsx";
import SettingsWidgetComponent from ".";

interface SettingsWidgetArgs {
  numberOfMiners: number;
}

export const SettingsWidget = ({ numberOfMiners }: SettingsWidgetArgs) => {
  const [hidden, setHidden] = useState(false);

  return (
    <div
      className={clsx("fixed top-40 left-40 rounded-3xl bg-grayscale-gray-87", {
        invisible: hidden,
      })}
    >
      <SettingsWidgetComponent
        numberOfMiners={numberOfMiners}
        setHidden={setHidden}
      />
    </div>
  );
};

export default {
  title: "Components (ProtoFleet)/Action Bar/Settings Widget",
  args: {
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
