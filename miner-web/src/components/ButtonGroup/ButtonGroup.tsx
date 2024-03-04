import { Fragment } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "components/Button";

import ButtonDivider from "./ButtonDivider";
import { groupVariants } from "./constants";
import { ButtonProps } from "./types";
import { sortPrimaryButtonFirst, sortPrimaryButtonLast } from "./utility";

interface ButtonGroupProps {
  buttons: ButtonProps[];
  className?: string;
  size?: keyof typeof sizes;
  sortButtons?: boolean;
  variant: keyof typeof groupVariants;
}

const ButtonGroup = ({
  buttons,
  className,
  size,
  sortButtons = true,
  variant,
}: ButtonGroupProps) => {
  const horizontalGap = "space-x-3";
  const verticalGap = "space-y-3";
  const parentClasses = ["flex"];

  const fill = variant === groupVariants.fill;
  const leftAligned = variant === groupVariants.leftAligned;
  const rightAligned = variant === groupVariants.rightAligned;
  const stack = variant === groupVariants.stack;
  const textOnly = variant === groupVariants.textOnly;

  let sortedButtons = buttons;

  if (fill) {
    parentClasses.push(...["w-full", horizontalGap]);
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonLast(buttons);
    }
  }

  if (leftAligned) {
    parentClasses.push(horizontalGap);
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonFirst(buttons);
    }
  }

  if (rightAligned) {
    parentClasses.push(horizontalGap);
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonLast(buttons);
    }
  }

  if (stack) {
    parentClasses.push(...["flex-col", verticalGap]);
    if (sortButtons) {
      sortedButtons = sortPrimaryButtonFirst(buttons);
    }
  }

  if (textOnly) {
    parentClasses.push("space-x-2");
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
            className={clsx({ grow: fill }, button.className)}
          />
          {textOnly && index !== sortedButtons.length - 1 && <ButtonDivider />}
        </Fragment>
      ))}
    </div>
  );
};

export default ButtonGroup;
