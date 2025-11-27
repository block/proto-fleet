import { CSSProperties } from "react";
import clsx from "clsx";

import { iconSizes } from "./constants";
import { IconProps } from "./types";

type CircleProps = IconProps & {
  style?: CSSProperties;
};

const Circle = ({ className, width = iconSizes.small, style }: CircleProps) => {
  // to match the behavior of other icons that are svgs that use color to set the color
  // we replace text- with bg- in the className because the div needs a background color
  const modifiedClassName = className?.replace(/(^|\s)text-/, "$1bg-");

  return (
    <div className={width}>
      <div
        style={style}
        className={clsx("aspect-square rounded-[50%]", modifiedClassName, width || iconSizes.xSmall)}
      />
    </div>
  );
};

export default Circle;
