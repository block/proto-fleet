import { ReactNode } from "react";
import clsx from "clsx";

import { popoverSizes } from "./constants";
import ButtonGroup, {
  ButtonProps,
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Header from "@/shared/components/Header";
import { positions } from "@/shared/constants";

import "./style.css";

interface PopoverProps {
  buttonGroupVariant?: keyof typeof groupVariants;
  buttons?: ButtonProps[];
  children?: ReactNode;
  className?: string;
  position?: keyof typeof positions;
  size?: keyof typeof popoverSizes;
  subtitle?: string;
  testId?: string;
  title?: string;
  titleSize?: string;
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
  titleSize = "text-heading-200",
}: PopoverProps) => {
  return (
    <div
      className={clsx(
        "p-6 rounded-3xl shadow-200 absolute bg-surface-elevated-base/85 backdrop-blur-[7px] space-y-4 z-20 transition-opacity duration-200",
        {
          "right-0 mt-2": position === positions["bottom left"],
          "bottom-0": position === positions["top right"],
          "animate-slide-down-popover": position?.includes("bottom"),
          "animate-slide-up-popover": position?.includes("top"),
          "w-60": size === popoverSizes.small,
          "w-72": size === popoverSizes.medium,
          "w-80": size === popoverSizes.normal,
        },
        className
      )}
      data-testid={testId}
    >
      {(title || subtitle) && (
        <Header
          title={title}
          titleSize={titleSize}
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
