import type { ReactNode } from "react";
import clsx from "clsx";

export const Stat = ({ label, value }: { label: string; value: ReactNode }) => (
  <div>
    <dt className="mb-1 text-300 text-text-primary-70">{label}</dt>
    <dd className="text-emphasis-400 text-text-primary tabular-nums">{value}</dd>
  </div>
);

export const Panel = ({ children, className }: { children: ReactNode; className?: string }) => (
  <div className={clsx("rounded-2xl bg-surface-overlay p-6", className)}>{children}</div>
);
