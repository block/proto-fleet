import type { ReactNode } from "react";
import { HashboardIndicatorV2 } from "@/shared/assets/icons";
import HardwareCardHeader from "@/shared/components/HardwareCardHeader";
import LabeledValue from "@/shared/components/LabeledValue";

interface HashboardCardProps {
  title: string;
  metrics: { label: string; value: ReactNode }[];
  position?: number;
  totalSlots?: number;
  threeColumns?: boolean;
  statusIcon?: ReactNode;
  onInfoClick?: () => void;
  actions?: ReactNode;
  children?: ReactNode;
}

/** Shared card presentation; each app owns its data, modal, and optional ASIC preview. */
export default function HashboardCard({
  title,
  metrics,
  position,
  totalSlots = 3,
  threeColumns = true,
  statusIcon,
  onInfoClick,
  actions,
  children,
}: HashboardCardProps) {
  return (
    <article
      aria-label={title}
      data-testid="card"
      className="@container flex h-full min-w-0 flex-col gap-6 rounded-xl bg-surface-overlay p-4"
    >
      <HardwareCardHeader
        title={title}
        titleAs="h3"
        infoAriaLabel={`${title} info`}
        statusIcon={statusIcon}
        componentIcon={
          position !== undefined ? (
            <HashboardIndicatorV2 width="w-4" position={position} totalSlots={totalSlots} />
          ) : null
        }
        onInfoIconClick={onInfoClick}
        actions={actions}
      />
      <div className="flex flex-col gap-3">
        {metrics.length ? (
          <div className={`grid grid-cols-2 gap-x-4 gap-y-3 ${threeColumns ? "@xs:grid-cols-3" : ""}`}>
            {metrics.map(({ label, value }) => (
              <LabeledValue key={label} label={label} value={value} />
            ))}
          </div>
        ) : null}
        {children}
      </div>
    </article>
  );
}
