import { ReactNode } from "react";
import clsx from "clsx";

interface WidgetWrapperProps {
  children: ReactNode;
  className?: string;
  isOpen?: boolean;
  onClick?: () => void;
  testId?: string;
  contrast?: boolean;
}

const WidgetWrapper = ({
  children,
  className,
  isOpen,
  onClick,
  testId,
  contrast = false,
}: WidgetWrapperProps) => {
  const baseClasses =
    "flex h-7 items-center rounded-2xl px-2 py-1 text-heading-50 whitespace-nowrap transition-[background-color] duration-200 ease-in-out";
  const bgColor = contrast
    ? "bg-core-primary-80 text-text-contrast"
    : "bg-surface-base text-text-primary";
  const hoverBgColor = contrast
    ? "hover:bg-core-primary-fill"
    : "hover:bg-core-primary-5";
  const isOpenClasses = isOpen
    ? "shadow-200"
    : `${bgColor} text-text-contrast shadow-50`;

  return (
    <button
      className={clsx(baseClasses, hoverBgColor, isOpenClasses, className)}
      onClick={onClick}
      data-testid={testId}
    >
      {children}
    </button>
  );
};

export default WidgetWrapper;
