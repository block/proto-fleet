import type { ReactNode } from "react";
import type { HomeHistory, IssueSummary, ModuleGrid } from "../types";
import ActiveIssues from "./ActiveIssues";
import ModuleStateGrid from "./ModuleStateGrid";
import ProductionChart from "./ProductionChart";
import Button from "@/shared/components/Button";
import DurationSelector, { type Duration } from "@/shared/components/DurationSelector";
import type { StatProps } from "@/shared/components/Stat/types";
import Stats from "@/shared/components/Stats";

const panel = "min-w-0 rounded-xl bg-surface-elevated-base p-6 shadow-100";

function Panel({
  title,
  action,
  className,
  children,
}: {
  title: string;
  action?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section aria-label={title} className={`${panel} flex flex-col gap-5 ${className ?? ""}`}>
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-heading-100 text-text-primary">{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}

const viewButton = { variant: "secondary", size: "compact" } as const;

export interface ProductionHistory {
  id: string;
  label: string;
  to: string;
  /** Extremes across every series in the chart. */
  low: string;
  high: string;
  history: HomeHistory;
}

interface Props {
  kpis: Omit<StatProps, "size">[];
  production: ProductionHistory[];
  historyError?: string;
  duration: Duration;
  onDurationChange: (duration: Duration) => void;
  grid?: ModuleGrid;
  issues: IssueSummary;
  links: { tanks: string };
  onViewIssues: () => void;
}

export default function ContainerOverviewView({
  kpis,
  production,
  historyError,
  duration,
  onDurationChange,
  grid,
  issues,
  links,
  onViewIssues,
}: Props) {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="sr-only">Container overview</h1>
      <div className={panel}>
        <Stats stats={kpis} size="medium" grid="grid-cols-2 laptop:grid-cols-4" gap="gap-x-10 gap-y-6" padding="" />
      </div>
      <section aria-label="Performance history" className={`${panel} flex flex-col gap-5`}>
        <div className="flex justify-end">
          <DurationSelector duration={duration} onSelect={onDurationChange} />
        </div>
        {historyError ? (
          <p role="alert" className="text-300 text-text-critical">
            {historyError}
          </p>
        ) : null}
        <div className="grid grid-cols-1 gap-8 laptop:grid-cols-2">
          {production.map(({ id, label, to, low, high, history }) => (
            <div key={id} className="flex min-w-0 flex-col gap-2">
              <div className="flex items-center justify-between gap-3">
                <h2 className="text-heading-100 text-text-primary">{label}</h2>
                <Button {...viewButton} text="View" to={to} ariaLabel={`View ${label}`} />
              </div>
              <p className="text-200 text-text-primary-50 tabular-nums">
                Low {low} · High {high}
              </p>
              <ProductionChart history={history} />
            </div>
          ))}
        </div>
      </section>
      <div className="grid grid-cols-1 gap-4 laptop:grid-cols-[2fr_1fr]">
        <Panel title="Module state" action={<Button {...viewButton} text="View tanks" to={links.tanks} />}>
          {grid ? <ModuleStateGrid grid={grid} /> : <p className="text-300 text-text-primary-50">No tanks reported.</p>}
        </Panel>
        <Panel
          title="Active issues"
          action={issues.groups.length ? <Button {...viewButton} text="View all" onClick={onViewIssues} /> : undefined}
        >
          <ActiveIssues issues={issues} />
        </Panel>
      </div>
    </div>
  );
}
