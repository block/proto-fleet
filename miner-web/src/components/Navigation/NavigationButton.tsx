import clsx from "clsx";

import Button from "components/Button";

interface NavigationButtonProps {
  className?: string;
  onClick: () => void;
  text: string;
}

const NavigationButton = ({
  className,
  onClick,
  text,
}: NavigationButtonProps) => {
  return (
    <Button
      text={text}
      className={clsx("w-full", className)}
      onClick={onClick}
    />
  );
};

export default NavigationButton;
