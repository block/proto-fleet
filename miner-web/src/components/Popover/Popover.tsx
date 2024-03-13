import { ReactNode } from "react";
import clsx from "clsx";

import { positions } from "common/constants";

import ButtonGroup, {
  ButtonProps,
  groupVariants,
  sizes,
} from "components/ButtonGroup";
import Header from "components/Header";

interface PopoverProps {
  buttons?: ButtonProps[];
  children?: ReactNode;
  className?: string;
  position?: keyof typeof positions;
  subtitle?: string;
  testId?: string;
  title?: string;
}

const Popover = ({
  buttons,
  children,
  className,
  position,
  subtitle,
  testId,
  title,
}: PopoverProps) => {
  return (
    <div
      className={clsx(
        "w-80 p-6 rounded-3xl shadow-200 absolute mt-2 bg-surface-base/85 backdrop-blur-[7px] space-y-4",
        {
          "right-0": position === positions["bottom left"],
        },
        className
      )}
      data-testid={testId}
    >
      <Header
        title={title}
        titleSize="text-heading-200"
        subtitle={subtitle}
        subtitleSize="text-300"
      />
      {children}
      {buttons && (
        <ButtonGroup
          buttons={buttons}
          variant={groupVariants.fill}
          size={sizes.base}
        />
      )}
    </div>
  );
};

export default Popover;
