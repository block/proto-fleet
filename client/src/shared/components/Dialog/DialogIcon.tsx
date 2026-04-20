import { ReactNode } from "react";
import clsx from "clsx";

const intentStyles = {
  info: "text-intent-info-fill",
  success: "text-intent-success-fill",
  warning: "text-intent-warning-fill",
  critical: "text-intent-critical-fill",
} as const;

type Intent = keyof typeof intentStyles;

interface DialogIconProps {
  children: ReactNode;
  intent?: Intent;
}

const DialogIcon = ({ children, intent }: DialogIconProps) => (
  <div
    className={clsx("flex size-10 items-center justify-center rounded-lg bg-surface-5", intent && intentStyles[intent])}
  >
    {children}
  </div>
);

export default DialogIcon;
