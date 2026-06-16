import { ReactNode } from "react";
import clsx from "clsx";

import { cardType } from ".";

interface CardProps {
  children: ReactNode;
  title: ReactNode;
  type: (typeof cardType)[keyof typeof cardType];
  bodyClassName?: string;
  className?: string;
  headerAction?: ReactNode;
  headerClassName?: string;
  headerTone?: "neutral" | "status";
  testId?: string;
  titleClassName?: string;
}

const Card = ({
  bodyClassName,
  children,
  className,
  headerAction,
  headerClassName,
  headerTone = "status",
  testId,
  title,
  titleClassName,
  type,
}: CardProps) => {
  return (
    <div className={clsx("rounded-xl shadow-50", className)} data-testid={testId}>
      <div
        className={clsx(
          "flex items-center justify-between gap-4 rounded-t-xl px-4 py-2",
          headerTone === "status" && {
            "bg-core-primary-5 text-text-primary": type === cardType.default,
            "bg-intent-success-fill text-text-contrast": type === cardType.success,
            "bg-intent-critical-fill text-text-contrast": type === cardType.warning,
          },
          headerClassName,
        )}
      >
        <div className={clsx("min-w-0", titleClassName)}>{title}</div>
        {headerAction ? <div className="shrink-0">{headerAction}</div> : null}
      </div>
      <div className={bodyClassName}>{children}</div>
    </div>
  );
};

export default Card;
