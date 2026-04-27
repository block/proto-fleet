import type { ReactNode } from "react";
import clsx from "clsx";

import type { ButtonVariant } from "@/shared/components/Button";
import Button, { variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";

export interface StatusBreakdownItem {
  key: string;
  color: string;
  label: string;
  icon?: ReactNode;
  percentageLabel: string;
  count: number;
  showButton?: boolean;
  buttonVariant?: ButtonVariant;
  onClick?: () => void;
}

interface StatusBreakdownPanelProps {
  items: StatusBreakdownItem[];
  className?: string;
}

export const StatusBreakdownPanel = ({ items, className }: StatusBreakdownPanelProps) => {
  return (
    <div
      className={clsx(
        "flex flex-col justify-between space-y-3 bg-transparent p-6 pt-0 laptop:p-10 laptop:pt-0 dark:bg-transparent",
        className,
      )}
    >
      {items.map((segment, idx) => (
        <div key={segment.key} className="relative flex grow flex-row items-center">
          {segment.icon ? (
            <span className="mr-3 flex" style={{ color: `var(${segment.color})` }}>
              {segment.icon}
            </span>
          ) : (
            <div className="mr-3 h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: `var(${segment.color})` }} />
          )}

          <div className="flex flex-1 flex-col">
            <span className="text-emphasis-300 text-text-primary">{segment.label}</span>
            <span className="text-300 text-text-primary-70">{segment.percentageLabel}</span>
          </div>

          {segment.showButton && segment.count > 0 ? (
            <Button
              variant={segment.buttonVariant ? variants[segment.buttonVariant] : variants.secondary}
              size="compact"
              onClick={segment.onClick}
              className={clsx({ "pointer-events-none": !segment.onClick })}
            >
              {segment.count} {segment.count === 1 ? "miner" : "miners"}
            </Button>
          ) : null}

          {idx < items.length - 1 ? <Divider className="absolute -bottom-4 left-0 w-full" /> : null}
        </div>
      ))}
    </div>
  );
};
