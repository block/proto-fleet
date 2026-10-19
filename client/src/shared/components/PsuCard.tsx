import type { ReactNode } from "react";
import HardwareCardHeader from "@/shared/components/HardwareCardHeader";
import LabeledValue from "@/shared/components/LabeledValue";

/** App-neutral PSU presentation; callers own telemetry, unit preferences, and status dialogs. */
export default function PsuCard({
  title,
  metrics,
  statusIcon,
  componentIcon,
  onInfoClick,
  infoAriaLabel,
}: {
  title: string;
  metrics: { label: string; value: ReactNode }[];
  statusIcon?: ReactNode;
  componentIcon?: ReactNode;
  onInfoClick?: () => void;
  infoAriaLabel?: string;
}) {
  return (
    <article
      aria-label={title}
      data-testid="card"
      className="@container flex min-w-0 flex-col gap-6 rounded-xl bg-surface-2 p-4 dark:bg-core-primary-5"
    >
      <HardwareCardHeader
        title={title}
        titleAs="h3"
        statusIcon={statusIcon}
        componentIcon={componentIcon}
        onInfoIconClick={onInfoClick}
        infoAriaLabel={infoAriaLabel}
      />
      <div className="grid grid-cols-2 gap-x-4 gap-y-3 @3xl:grid-cols-6">
        {metrics.map((stat) => (
          <LabeledValue key={stat.label} {...stat} />
        ))}
      </div>
    </article>
  );
}
