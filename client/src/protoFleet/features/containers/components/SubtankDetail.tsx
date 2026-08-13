import { useState } from "react";

import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { ModuleActionType } from "@/protoFleet/features/containers/components/moduleActions";
import TankHealthModule from "@/protoFleet/features/containers/components/TankHealthModule";
import type { TankModuleState } from "@/protoFleet/features/containers/components/TankModuleGrid";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import Breadcrumb, { type BreadcrumbSegment } from "@/shared/components/Breadcrumb";
import { sizes, variants } from "@/shared/components/Button";
import DurationSelector, { type FleetDuration, fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import type { StatProps } from "@/shared/components/Stat/types";
import Stats from "@/shared/components/Stats";

/** KPI header stat, minus the required `size` which the header sets uniformly. */
type SubtankKpi = Omit<StatProps, "size">;

export interface SubtankDetailProps {
  breadcrumb: BreadcrumbSegment[];
  title: string;
  /** Optional subtitle under the title (e.g. cooling type), like the rack page's zone. */
  subtitle?: string;
  kpis: SubtankKpi[];
  /** Module-bar grid dimensions (a 16-module tank reads as 8 × 2). */
  rows: number;
  cols: number;
  /** One entry per module bar, row-major — the same array the overview TankCard renders. */
  modules: TankModuleState[];
  /** Tank PDU state; when false every populated module reads offline. */
  on: boolean;
  /** Short tank label for per-module accessible names (e.g. "Tank 2"); defaults to `title`. */
  tankLabel?: string;
  /**
   * Telemetry for the subtank's performance charts. Undefined = still loading
   * (skeletons); empty = no data. Passed straight through to the shared
   * DeviceSetPerformanceSection used by the rack and container detail views.
   */
  metrics?: Metric[];
  onViewMiners?: () => void;
  /**
   * When supplied, each module bar exposes the same action menu (View + Blink
   * LEDs / Reboot / Sleep) the overview tank card offers.
   */
  onModuleAction?: (moduleIndex: number, action: ModuleActionType) => void;
}

/**
 * Subtank detail — a single tank rendered as the tank analogue of
 * RackOverviewPage: Breadcrumb + Header title bar, a KPI header, the
 * TankHealthModule (the tank's own 3-segment module bars + status breakdown),
 * and the shared DeviceSetPerformanceSection. Where the rack page resolves a
 * DeviceSet from the backend, this is presentational and prop-driven
 * (prototype-first) — a tank has no DeviceSet yet.
 *
 * Per jmarr (2026-08-10) the health section keeps the tank's module language
 * (the same ModuleTile bars the overview card uses, scaled up and numbered)
 * rather than the Fleet numbered-slot grid, so the card and the detail read as
 * one surface. See TankHealthModule.
 */
const SubtankDetail = ({
  breadcrumb,
  title,
  subtitle,
  kpis,
  rows,
  cols,
  modules,
  on,
  tankLabel,
  metrics,
  onViewMiners,
  onModuleAction,
}: SubtankDetailProps) => {
  const [duration, setDuration] = useState<FleetDuration>("24h");

  const headerButtons = [
    {
      variant: variants.secondary,
      text: "View miners",
      onClick: onViewMiners ?? (() => undefined),
      disabled: !onViewMiners,
      testId: "subtank-detail-view-miners",
    },
  ];

  return (
    <div className="flex flex-col gap-10 p-6 laptop:p-10" data-testid="subtank-detail">
      {/* Title bar */}
      <div className="flex flex-col gap-3">
        <Breadcrumb segments={breadcrumb} testId="subtank-detail-breadcrumb" />
        <Header
          title={title}
          titleSize="truncate text-heading-300"
          subtitle={subtitle}
          subtitleSize="text-300"
          subtitleClassName="text-text-primary"
          inline
          centerButton
          stackButtonsOnPhone={false}
          buttons={headerButtons}
          buttonSize={sizes.compact}
          testId="subtank-detail-title"
        />
      </div>

      {/* KPI header */}
      <Stats
        stats={kpis}
        size="large"
        grid="grid-cols-4 phone:grid-cols-2 phone:gap-y-6"
        gap="gap-x-10 phone:gap-6"
        padding=""
      />

      {/* Health overview — the tank's own 3-segment module bars + breakdown */}
      <section data-testid="subtank-detail-health">
        <TankHealthModule
          rows={rows}
          cols={cols}
          modules={modules}
          on={on}
          label={tankLabel ?? title}
          onModuleAction={onModuleAction}
        />
      </section>

      {/* Performance — identical wiring to the rack and container detail views */}
      <section className="flex flex-col gap-4" data-testid="subtank-detail-performance">
        <div className="flex flex-col gap-3 tablet:flex-row tablet:items-center tablet:justify-between">
          <div className="text-heading-200 text-text-primary">Performance</div>
          <div className="flex items-center gap-3 text-200 text-core-primary-50">
            <div className="flex items-center gap-2">
              <svg width="24" height="4">
                <line
                  x1="0"
                  y1="2"
                  x2="24"
                  y2="2"
                  stroke="var(--color-core-primary-fill)"
                  strokeWidth="3"
                  strokeLinecap="round"
                />
              </svg>
              <span>Tank</span>
            </div>
            <div className="flex items-center gap-2">
              <svg width="24" height="4">
                <line
                  x1="0"
                  y1="2"
                  x2="24"
                  y2="2"
                  stroke="var(--color-core-primary-50)"
                  strokeWidth="3"
                  strokeLinecap="round"
                  strokeDasharray="1 6"
                  strokeOpacity="0.5"
                />
              </svg>
              <span>Max</span>
            </div>
            <div className="flex items-center gap-2">
              <svg width="24" height="4">
                <line
                  x1="0"
                  y1="2"
                  x2="24"
                  y2="2"
                  stroke="var(--color-intent-critical-fill)"
                  strokeWidth="3"
                  strokeLinecap="round"
                  strokeDasharray="1 6"
                  strokeOpacity="0.5"
                />
              </svg>
              <span>Min</span>
            </div>
          </div>
          <div className="flex items-center">
            <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
          </div>
        </div>
        <DeviceSetPerformanceSection duration={duration} gapClassName="gap-4" metrics={metrics} />
      </section>
    </div>
  );
};

export default SubtankDetail;
export type { SubtankDetailProps as SubtankDetailComponentProps };
