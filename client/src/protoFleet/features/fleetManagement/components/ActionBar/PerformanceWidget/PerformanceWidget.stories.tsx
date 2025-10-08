import { useState } from "react";
import clsx from "clsx";
import PerformanceWidgetComponent from ".";

interface PerformanceWidgetArgs {
  numberOfMiners: number;
}

export const PerformanceWidget = ({
  numberOfMiners,
}: PerformanceWidgetArgs) => {
  const [hidden, setHidden] = useState(false);

  return (
    <div
      className={clsx("fixed top-40 left-40 rounded-3xl bg-grayscale-gray-87", {
        invisible: hidden,
      })}
    >
      <PerformanceWidgetComponent
        numberOfMiners={numberOfMiners}
        setHidden={setHidden}
      />
    </div>
  );
};

export default {
  title: "Proto Fleet/Action Bar/Performance Widget",
  args: {
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
