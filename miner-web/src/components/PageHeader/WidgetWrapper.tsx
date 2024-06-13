import { ReactNode } from "react";
import clsx from "clsx";

interface WidgetWrapperProps {
  children: ReactNode;
  className?: string;
  isOpen?: boolean;
  onClick?: () => void;
}

const WidgetWrapper = ({
  children,
  className,
  isOpen,
  onClick,
}: WidgetWrapperProps) => {
  return (
    <button
      className={clsx(
        "text-heading-50 rounded-md bg-surface-base px-2 py-1 flex items-center whitespace-nowrap",
        { "shadow-50": !isOpen },
        { "shadow-200": isOpen },
        className
      )}
      onClick={onClick}
    >
      {children}
    </button>
  );
};

export default WidgetWrapper;
