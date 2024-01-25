import clsx from "clsx";

import Button from "components/Button";

interface NavigationButtonProps {
  className?: string;
  icon?: string;
  onClick: () => void;
  text: string;
}

const NavigationButton = ({
  className,
  icon,
  onClick,
  text,
}: NavigationButtonProps) => {
  return (
    <Button
      text={text}
      className={clsx("w-full", className)}
      icon={icon}
      onClick={onClick}
    />
  );
};

export default NavigationButton;
