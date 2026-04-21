import { ReactNode } from "react";
import clsx from "clsx";

interface LabeledValueProps {
  value: ReactNode;
  label: string;
  variant?: "base" | "large";
}

function LabeledValue({ value, label, variant = "base" }: LabeledValueProps) {
  return (
    <div>
      <div
        className={clsx("text-text-primary", {
          "text-emphasis-300": variant === "base",
          "text-heading-200": variant === "large",
        })}
      >
        {value}
      </div>
      <div className="text-300 text-text-primary-50">{label}</div>
    </div>
  );
}

export default LabeledValue;
