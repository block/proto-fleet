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
        "text-heading-50 rounded-md px-2 py-1 flex items-center whitespace-nowrap",
        "hover:bg-core-primary-5 transition-[background-color] ease-in-out duration-200",
        { "shadow-50 bg-surface-base": !isOpen },
        { "shadow-200 bg-core-primary-5": isOpen },
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
