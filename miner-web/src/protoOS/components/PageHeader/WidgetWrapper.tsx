import { ReactNode } from "react";
import clsx from "clsx";

interface WidgetWrapperProps {
  children: ReactNode;
  className?: string;
  isOpen?: boolean;
  onClick?: () => void;
  testId?: string;
}

const WidgetWrapper = ({
  children,
  className,
  isOpen,
  onClick,
  testId,
}: WidgetWrapperProps) => {
  return (
    <button
      className={clsx(
        "flex items-center rounded-md px-2 py-1 text-heading-50 whitespace-nowrap",
        "transition-[background-color] duration-200 ease-in-out hover:bg-core-primary-5",
        { "bg-surface-base shadow-50": !isOpen },
        { "bg-core-primary-5 shadow-200": isOpen },
        className,
      )}
      onClick={onClick}
      data-testid={testId}
    >
      {children}
    </button>
  );
};

export default WidgetWrapper;
