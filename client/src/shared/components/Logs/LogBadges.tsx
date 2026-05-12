import { MouseEvent } from "react";
import clsx from "clsx";

import { DismissTiny } from "@/shared/assets/icons";

interface LogBadgesProps {
  className: string;
  count: number;
  label: string;
  onClick: (e: MouseEvent<HTMLDivElement>) => void;
  selected: boolean;
  testId?: string;
}

const LogBadges = ({ className, count, label, onClick, selected, testId }: LogBadgesProps) => {
  return (
    <div
      className={clsx("cursor-pointer rounded-lg border text-emphasis-300 whitespace-nowrap", className)}
      onClick={onClick}
      data-testid={testId}
    >
      <div className="flex items-center px-2 py-[1px]">
        {count} {label}
        {selected ? <DismissTiny className="ml-2" /> : null}
      </div>
    </div>
  );
};

export default LogBadges;
