import type { ReactNode } from "react";
import HardwareCardHeader from "@/shared/components/HardwareCardHeader";
import LabeledValue from "@/shared/components/LabeledValue";

type Metric = { label: string; value: ReactNode };

/** App-neutral PSU presentation; callers own telemetry, unit preferences, and status dialogs. */
export default function PsuCard({
  title,
  metrics = [],
  groups,
  status,
  statusIcon,
  componentIcon,
  onInfoClick,
  infoAriaLabel,
}: {
  title: string;
  metrics?: Metric[];
  groups?: { label: string; metrics: Metric[]; status?: string }[];
  status?: string;
  statusIcon?: ReactNode;
  componentIcon?: ReactNode;
  onInfoClick?: () => void;
  infoAriaLabel?: string;
}) {
  return (
    <article
      aria-label={title}
      data-testid="card"
      className="@container flex min-w-0 flex-col gap-6 rounded-xl bg-surface-overlay p-4"
    >
      <HardwareCardHeader
        title={title}
        titleAs="h3"
        statusIcon={statusIcon}
        componentIcon={componentIcon}
        onInfoIconClick={onInfoClick}
        infoAriaLabel={infoAriaLabel}
      />
      {groups ? (
        <div className="grid gap-6 @lg:grid-cols-2 @3xl:grid-cols-3">
          {groups.map((group) => (
            <section key={group.label} aria-label={group.label} className="min-w-0 space-y-3">
              <h4 className="text-emphasis-300 text-text-primary">{group.label}</h4>
              <div className="grid grid-cols-3 gap-x-4 gap-y-3">
                {group.metrics.map((metric) => (
                  <LabeledValue key={metric.label} {...metric} />
                ))}
              </div>
              {group.status ? (
                <p role="status" className="text-300 text-text-primary-70">
                  {group.status}
                </p>
              ) : null}
            </section>
          ))}
        </div>
      ) : null}
      {metrics.length ? (
        <div className="grid grid-cols-2 gap-x-4 gap-y-3 @3xl:grid-cols-6">
          {metrics.map((stat) => (
            <LabeledValue key={stat.label} {...stat} />
          ))}
        </div>
      ) : null}
      {status ? (
        <p role="status" className="text-300 text-text-primary-70">
          {status}
        </p>
      ) : null}
    </article>
  );
}
