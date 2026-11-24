import type { ReactNode } from "react";

const colors = {
  info: "text-text-primary-30",
  critical: "text-intent-critical-fill",
  warning: "text-intent-warning-fill",
  success: "text-intent-success-fill",
};

export default function StatusModalIcon({
  children,
  intent = "info",
}: {
  children: ReactNode;
  intent?: keyof typeof colors;
}) {
  return <div className={`flex size-8 items-center justify-center ${colors[intent]}`}>{children}</div>;
}
