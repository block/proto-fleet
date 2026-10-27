import { ReactNode } from "react";
import clsx from "clsx";
import type { StatProps } from "@/shared/components/Stat";
import Stats from "@/shared/components/Stats";

type ChartWidgetStat = Omit<StatProps, "size">;

type ChartWidgetProps = {
  surface?: "elevated" | "overlay";
  stats?: ChartWidgetStat | ChartWidgetStat[];
  children: ReactNode;
  className?: string;
  statsGrid?: string;
  statsGap?: string;
  statsPadding?: string;
  statsSize?: StatProps["size"];
};

const ChartWidget = ({
  surface = "elevated",
  stats,
  children,
  className,
  statsGrid = "grid-cols-1",
  statsGap = "gap-4",
  statsPadding = "pb-6",
  statsSize = "large",
}: ChartWidgetProps) => {
  // Normalize stats to always be an array
  const statsArray = stats ? (Array.isArray(stats) ? stats : [stats]) : [];

  return (
    <div
      className={clsx(
        "rounded-xl p-10 phone:p-6",
        surface === "overlay" ? "bg-surface-overlay" : "bg-surface-elevated-base shadow-100",
        className,
      )}
    >
      <div className={statsPadding}>
        {statsArray.length > 0 ? (
          <Stats
            stats={statsArray}
            size={statsSize}
            grid={statsGrid}
            gap={statsGap}
            padding="" // let our parent handle padding
          />
        ) : null}
      </div>
      <div className="flex w-full">{children}</div>
    </div>
  );
};

export default ChartWidget;
