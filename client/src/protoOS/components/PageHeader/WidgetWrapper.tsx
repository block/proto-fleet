import { ReactNode } from "react";
import clsx from "clsx";
import Button, { type ButtonVariant, sizes, variants } from "@/shared/components/Button";

interface WidgetWrapperProps {
  children: ReactNode;
  className?: string;
  isOpen?: boolean;
  onClick?: () => void;
  testId?: string;
  variant?: ButtonVariant;
  textColor?: string;
  borderColor?: string;
  ariaLabel?: string;
  ariaHasPopup?: boolean | "menu" | "dialog" | "listbox" | "tree" | "grid";
  ariaExpanded?: boolean;
}

const WidgetWrapper = ({
  children,
  className,
  onClick,
  testId,
  variant,
  textColor,
  borderColor,
  ariaLabel,
  ariaHasPopup,
  ariaExpanded,
}: WidgetWrapperProps) => {
  const baseClasses = "flex h-7 items-center rounded-2xl px-2 py-1 whitespace-nowrap";

  return (
    <Button
      variant={variant || variants.secondary}
      textColor={textColor}
      borderColor={borderColor}
      size={sizes.compact}
      className={clsx(baseClasses, className)}
      onClick={onClick}
      testId={testId}
      ariaLabel={ariaLabel}
      ariaHasPopup={ariaHasPopup}
      ariaExpanded={ariaExpanded}
    >
      <div className="flex gap-2">{children}</div>
    </Button>
  );
};

export default WidgetWrapper;
