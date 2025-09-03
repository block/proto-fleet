import { ReactNode } from "react";
import clsx from "clsx";
import Button, { sizes, variants } from "@/shared/components/Button";

interface WidgetWrapperProps {
  children: ReactNode;
  className?: string;
  isOpen?: boolean;
  onClick?: () => void;
  testId?: string;
  variant?: keyof typeof variants;
}

const WidgetWrapper = ({
  children,
  className,
  onClick,
  testId,
  variant,
}: WidgetWrapperProps) => {
  const baseClasses =
    "flex h-7 items-center rounded-2xl px-2 py-1 whitespace-nowrap";

  return (
    <Button
      variant={variant || variants.secondary}
      size={sizes.compact}
      className={clsx(baseClasses, className)}
      onClick={onClick}
      data-testid={testId}
    >
      <div className="flex gap-2">{children}</div>
    </Button>
  );
};

export default WidgetWrapper;
