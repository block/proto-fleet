import { useState } from "react";
import clsx from "clsx";
import DeviceWidgetComponent from ".";

interface DeviceWidgetArgs {
  numberOfMiners: number;
}

export const DeviceWidget = ({ numberOfMiners }: DeviceWidgetArgs) => {
  const [hidden, setHidden] = useState(false);

  return (
    <div
      className={clsx("fixed top-64 left-40 rounded-3xl bg-grayscale-gray-87", {
        invisible: hidden,
      })}
    >
      <DeviceWidgetComponent
        selectedMiners={Array(numberOfMiners).fill("MinerId")}
        setHidden={setHidden}
      />
    </div>
  );
};

export default {
  title: "Components (ProtoFleet)/Action Bar/Device Widget",
  args: {
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
