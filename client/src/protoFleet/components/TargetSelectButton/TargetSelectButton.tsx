import clsx from "clsx";

import { targetSelectPlaceholderLabel } from "./targetSelectButtonLabels";
import { ChevronDown } from "@/shared/assets/icons";

interface TargetSelectButtonProps {
  label: string;
  value: string;
  onClick: () => void;
}

function TargetSelectButton({ label, value, onClick }: TargetSelectButtonProps) {
  const isPlaceholder = value === targetSelectPlaceholderLabel;

  return (
    <button
      type="button"
      onClick={onClick}
      className="relative flex h-14 w-full items-center justify-between rounded-lg border border-border-5 bg-surface-base pr-4 pl-4 text-left outline-hidden"
    >
      <div className="flex min-w-0 flex-col pt-[18px]">
        <span className="absolute top-[7px] text-200 text-text-primary-50">{label}</span>
        <div className={clsx("truncate text-300", isPlaceholder ? "text-text-primary-50" : "text-text-primary")}>
          {value}
        </div>
      </div>
      <ChevronDown width="w-3" className="shrink-0 text-text-primary-70" />
    </button>
  );
}

export default TargetSelectButton;
