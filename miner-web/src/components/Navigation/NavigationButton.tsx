import { ReactNode } from "react";
import clsx from "clsx";

import Button, { sizes, variants } from "components/Button";

interface NavigationButtonProps {
  className?: string;
  prefixIcon?: ReactNode;
  onClick: () => void;
  text: string;
}

const NavigationButton = ({
  className,
  prefixIcon,
  onClick,
  text,
}: NavigationButtonProps) => {
  return (
    <Button
      text={text}
      className={clsx("w-full", className)}
      prefixIcon={prefixIcon}
      onClick={onClick}
      size={sizes.compact}
      variant={variants.secondary}
    />
  );
};

export default NavigationButton;
