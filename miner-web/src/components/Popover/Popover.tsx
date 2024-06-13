import { ReactNode } from "react";
import clsx from "clsx";

import { positions } from "common/constants";

import ButtonGroup, {
  ButtonProps,
  groupVariants,
  sizes,
} from "components/ButtonGroup";
import Header from "components/Header";

import { popoverSizes } from "./constants";
import "./style.css";

interface PopoverProps {
  buttonGroupVariant?: keyof typeof groupVariants;
  buttons?: ButtonProps[];
  children?: ReactNode;
  className?: string;
  position: keyof typeof positions;
  size?: keyof typeof popoverSizes;
  subtitle?: string;
  testId?: string;
  title?: string;
}

const Popover = ({
  buttonGroupVariant = groupVariants.fill,
  buttons,
  children,
  className,
  position,
  size = popoverSizes.normal,
  subtitle,
  testId,
  title,
}: PopoverProps) => {
  return (
    <div
      className={clsx(
        "p-6 rounded-3xl shadow-200 absolute bg-surface-base/85 backdrop-blur-[7px] space-y-4 z-20 transition-opacity duration-200",
        {
          "right-0 mt-2": position === positions["bottom left"],
          "bottom-0": position === positions["top right"],
          "animate-slide-down-popover": position?.includes("bottom"),
          "animate-slide-up-popover": position?.includes("top"),
          "w-60": size === popoverSizes.small,
          "w-80": size === popoverSizes.normal,
        },
        className
      )}
      data-testid={testId}
    >
      {(title || subtitle) && (
        <Header
          title={title}
          titleSize="text-heading-200"
          subtitle={subtitle}
          subtitleSize="text-300"
        />
      )}
      {children}
      {buttons && (
        <ButtonGroup
          buttons={buttons}
          variant={buttonGroupVariant}
          size={sizes.base}
        />
      )}
    </div>
  );
};

export default Popover;
