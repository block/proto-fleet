import type { HomeHistory, IssueSummary, ModuleGrid } from "../types";
import ActiveIssues from "./ActiveIssues";
import ModuleStateGrid from "./ModuleStateGrid";
import ProductionChart from "./ProductionChart";
import Button from "@/shared/components/Button";
import ChartWidget from "@/shared/components/ChartWidget";
import DurationSelector, { type Duration } from "@/shared/components/DurationSelector";
import type { StatProps } from "@/shared/components/Stat/types";

const panel = "min-w-0 rounded-xl bg-surface-elevated-base p-6 shadow-100";

const viewButton = { variant: "secondary", size: "compact" } as const;

export interface ProductionHistory {
  id: string;
  label: string;
  stats: Omit<StatProps, "size"> | Omit<StatProps, "size">[];
  history: HomeHistory;
}

interface Props {
  production: ProductionHistory[];
  historyError?: string;
  duration: Duration;
  onDurationChange: (duration: Duration) => void;
  grid?: ModuleGrid;
  issues: IssueSummary;
  onViewIssues: () => void;
  onSelectModule?: (id: string) => void;
}

export default function ContainerOverviewView({
  production,
  historyError,
  duration,
  onDurationChange,
  grid,
  issues,
  onViewIssues,
  onSelectModule,
}: Props) {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="sr-only">Container overview</h1>
      <section aria-label="Performance history" className="flex flex-col gap-4 pb-8">
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-heading-100 text-text-primary">Performance</h2>
          <DurationSelector duration={duration} onSelect={onDurationChange} />
        </div>
        {historyError ? (
          <p role="alert" className="text-300 text-text-critical">
            {historyError}
          </p>
        ) : null}
        <div className="grid grid-cols-1 gap-4 tablet:grid-cols-2">
          {production.map(({ id, label, stats, history }) => (
            <section key={id} aria-label={label} className="min-w-0">
              <ChartWidget
                surface="elevated"
                stats={stats}
                statsGrid={Array.isArray(stats) ? "grid-cols-1 desktop:grid-cols-3" : "grid-cols-1"}
              >
                <div className="flex w-full min-w-0 flex-col gap-4">
                  <ProductionChart history={history} />
                </div>
              </ChartWidget>
            </section>
          ))}
        </div>
      </section>
      <div className="flex flex-col gap-4">
        <section aria-label="Module state" className="flex min-w-0 flex-col gap-5 pb-8">
          <h2 className="text-heading-100 text-text-primary">Module state</h2>
          {grid ? (
            <ModuleStateGrid grid={grid} onSelectModule={onSelectModule} />
          ) : (
            <p className="text-300 text-text-primary-50">No tanks reported.</p>
          )}
        </section>
        <section aria-label="Active issues" className="flex min-w-0 flex-col gap-5">
          <div className="flex items-center justify-between gap-4">
            <h2 className="text-heading-100 text-text-primary">Active issues</h2>
            {issues.groups.length ? <Button {...viewButton} text="View all" onClick={onViewIssues} /> : null}
          </div>
          <div className={panel}>
            <ActiveIssues issues={issues} />
          </div>
        </section>
      </div>
    </div>
  );
}
