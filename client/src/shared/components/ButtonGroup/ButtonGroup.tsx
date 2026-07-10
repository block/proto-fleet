import { Fragment } from "react";
import clsx from "clsx";

import ButtonDivider from "./ButtonDivider";
import { groupVariants } from "./constants";
import { ButtonProps } from "./types";
import { sortPrimaryButtonFirst, sortPrimaryButtonLast } from "./utility";
import Button, { type sizes, variants } from "@/shared/components/Button";

interface ButtonGroupProps {
  buttons: ButtonProps[];
  className?: string;
  fillButtonsOnMobile?: boolean;
  fullWidthButtons?: boolean;
  fullWidthOnMobile?: boolean;
  size?: keyof typeof sizes;
  sortButtons?: boolean;
  variant: keyof typeof groupVariants;
}

const ButtonGroup = ({
  buttons,
  className,
  fillButtonsOnMobile = false,
  fullWidthButtons = false,
  fullWidthOnMobile = false,
  size,
  sortButtons = true,
  variant,
}: ButtonGroupProps) => {
  const fill = variant === groupVariants.fill;
  const justifyBetween = variant === groupVariants.justifyBetween;
  const leftAligned = variant === groupVariants.leftAligned;
  const rightAligned = variant === groupVariants.rightAligned;
  const stack = variant === groupVariants.stack;
  const textOnly = variant === groupVariants.textOnly;
  const gap = textOnly ? "gap-2" : "gap-3";
  const parentClasses = ["flex", gap, "phone:flex-wrap"];
  const shouldFillButtons = fill || stack || fullWidthButtons;

  let sortedButtons = buttons;

  if (stack || fullWidthButtons) {
    parentClasses.push("w-full");
  }

  if (fullWidthOnMobile) {
    parentClasses.push("phone:w-full");
  }

  if (fillButtonsOnMobile) {
    parentClasses.push("phone:w-full");
  }

  if (fill) {
    parentClasses.push("w-full");
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonLast(buttons);
    }
  }

  if (justifyBetween) {
    parentClasses.push(...["w-full", "justify-between"]);
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonLast(buttons);
    }
  }

  if (leftAligned) {
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonFirst(buttons);
    }
  }

  if (rightAligned) {
    parentClasses.push("justify-end");
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonLast(buttons);
    }
  }

  if (stack) {
    parentClasses.push("flex-col");
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonFirst(buttons);
    }
  }

  if (textOnly) {
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonLast(buttons);
    }
  }

  return (
    <div className={clsx(parentClasses, className)}>
      {sortedButtons.map((button, index) => (
        <Fragment key={index}>
          <Button
            {...button}
            size={size}
            variant={textOnly ? variants.textOnly : button.variant}
            className={clsx(
              {
                grow: fill,
                "w-full": shouldFillButtons,
                "phone:flex-1": fillButtonsOnMobile,
                "phone:w-full": fullWidthOnMobile,
              },
              button.className,
            )}
          />
          {textOnly && index !== sortedButtons.length - 1 ? <ButtonDivider /> : null}
        </Fragment>
      ))}
    </div>
  );
};

export default ButtonGroup;
