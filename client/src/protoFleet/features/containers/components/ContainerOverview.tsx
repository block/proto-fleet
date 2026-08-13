import { useState } from "react";
import ContainerControls, { type ContainerToggleControl } from "./ContainerControls";
import ContainerStatusModal from "./ContainerStatusModal";
import FanCard from "./FanCard";
import type { ModuleActionType } from "./moduleActions";
import TankCard from "./TankCard";
import type { TankModuleState } from "./TankModuleGrid";
import TankPowerConfirmationDialog from "./TankPowerConfirmationDialog";
import type { Metric } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import Breadcrumb, { type BreadcrumbSegment } from "@/shared/components/Breadcrumb";
import { sizes, variants } from "@/shared/components/Button";
import DurationSelector, { type FleetDuration, fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import type { StatProps } from "@/shared/components/Stat/types";
import Stats from "@/shared/components/Stats";

export interface ContainerTank {
  id: string;
  label: string;
  on: boolean;
  /** Module-bar grid dimensions (16 modules → 8 × 2). */
  cols: number;
  rows: number;
  /** One entry per module bar (two-state: healthy vs needs-attention). */
  modules: TankModuleState[];
  /** Footer readouts, e.g. ["48/48 boards", "65.5°", "12.3 kW"]. */
  stats: string[];
  /**
   * Temperature readout for the (ⓘ) status glance, mirroring the card footer
   * (e.g. "65.5°"). Optional — the glance shows a dash when absent.
   */
  tempLabel?: string;
  /**
   * Power readout for the (ⓘ) status glance, mirroring the card footer
   * (e.g. "12.3 kW"). Optional — the glance shows a dash when absent.
   */
  powerLabel?: string;
}

export interface ContainerFan {
  id: string;
  label: string;
  on: boolean;
  /** Fan speed as a percentage of max, 0–100. */
  speedPercent: number;
  /** Human-readable speed readout, e.g. "3,200". */
  speedLabel: string;
}

/** KPI header stat, minus the required `size` which the header sets uniformly. */
type ContainerKpi = Omit<StatProps, "size">;

export interface ContainerOverviewProps {
  breadcrumb: BreadcrumbSegment[];
  title: string;
  kpis: ContainerKpi[];
  tanks: ContainerTank[];
  fans: ContainerFan[];
  /** Container-level auxiliary equipment controls. Omit until the caller can supply them. */
  controls?: ContainerToggleControl[];
  /**
   * Telemetry for the container's performance charts. Undefined = still
   * loading (skeletons); empty = no data. Passed straight through to the shared
   * DeviceSetPerformanceSection used by the rack and building detail views.
   */
  metrics?: Metric[];
  onToggleTank: (id: string, on: boolean) => void;
  onToggleFan: (id: string, on: boolean) => void;
  onToggleControl?: (id: string, on: boolean) => void;
  onResetAlarm?: () => void;
  onMuteAlarm?: () => void;
  /**
   * Optional override for a tank card's (ⓘ). When omitted, the (ⓘ) opens the
   * built-in tank-level status glance (module health breakdown + temp/power)
   * reusing the shared StatusModal framework; the full drill-down stays the
   * Subtank detail page reached via onSelectTank (card-body click). Supply this
   * to intercept the (ⓘ) click instead.
   */
  onTankInfo?: (id: string) => void;
  /**
   * Optional override for a fan card's (ⓘ). When omitted, the (ⓘ) opens the
   * built-in component-status glance (speed / PWM + running state) reusing the
   * shared StatusModal framework; supply this to intercept the click instead.
   */
  onFanInfo?: (id: string) => void;
  onSelectTank?: (id: string) => void;
  /**
   * Fired when a module action is chosen from a tank module tile's popover
   * menu (View + Blink LEDs / Reboot / Sleep), with the tank id, the module's
   * row-major index within that tank, and the chosen action. When omitted the
   * module bars are plain, non-interactive status indicators.
   */
  onModuleAction?: (tankId: string, moduleIndex: number, action: ModuleActionType) => void;
  onViewMiners?: () => void;
  onViewDetails?: () => void;
}

/**
 * Container overview / tank heatmap (Frame 1). Composes existing primitives:
 * Breadcrumb + Header for the title bar, Stats for the KPI row, elevated
 * shadow panels group Tanks, Fans, and container-only auxiliary Controls before
 * the shared DeviceSetPerformanceSection used by rack/building detail. Tanks
 * use the distinct TankCard/TankModuleGrid visual (the tank analogue of
 * rack-detail's RackHealthModule) in a roomier tank grid; fans sit in a 5×2
 * grid that flexes to fill the panel. Presentational and controlled — all
 * operational state and handlers are owned by the caller, with no device check
 * or store read inside this tree.
 */
const ContainerOverview = ({
  breadcrumb,
  title,
  kpis,
  tanks,
  fans,
  controls,
  metrics,
  onToggleTank,
  onToggleFan,
  onToggleControl,
  onResetAlarm,
  onMuteAlarm,
  onTankInfo,
  onFanInfo,
  onSelectTank,
  onModuleAction,
  onViewMiners,
  onViewDetails,
}: ContainerOverviewProps) => {
  const [duration, setDuration] = useState<FleetDuration>("24h");
  // A tank toggle drives the tank's PDU (line power to every module), so it is
  // confirmed before firing. Holds the pending {id, label, next-on} until the
  // operator confirms or cancels the destructive remote action.
  const [pendingTank, setPendingTank] = useState<{ id: string; label: string; on: boolean } | null>(null);
  // The fan whose (ⓘ) is open in the component-status glance, or null when
  // closed. Owned here (like pendingTank) so the glance is self-contained; the
  // caller can still intercept via onFanInfo.
  const [statusFanId, setStatusFanId] = useState<string | null>(null);
  // The tank whose (ⓘ) is open in the component-status glance, or null when
  // closed. The (ⓘ) is a quick tank-level summary; the full drill-down is the
  // Subtank detail page (onSelectTank, card-body click). Caller can intercept
  // via onTankInfo.
  const [statusTankId, setStatusTankId] = useState<string | null>(null);

  const confirmTankToggle = () => {
    if (!pendingTank) return;
    onToggleTank(pendingTank.id, pendingTank.on);
    setPendingTank(null);
  };

  const statusFan = statusFanId !== null ? (fans.find((fan) => fan.id === statusFanId) ?? null) : null;
  const statusTank = statusTankId !== null ? (tanks.find((tank) => tank.id === statusTankId) ?? null) : null;

  const headerButtons = [
    {
      variant: variants.secondary,
      text: "View miners",
      onClick: onViewMiners ?? (() => undefined),
      disabled: !onViewMiners,
      testId: "container-overview-view-miners",
    },
    {
      variant: variants.secondary,
      text: "View details",
      onClick: onViewDetails ?? (() => undefined),
      disabled: !onViewDetails,
      testId: "container-overview-view-details",
    },
  ];

  return (
    <div className="flex flex-col gap-10 p-6 laptop:p-10" data-testid="container-overview">
      {/* Title bar */}
      <div className="flex flex-col gap-3">
        <Breadcrumb segments={breadcrumb} testId="container-overview-breadcrumb" />
        <Header
          title={title}
          titleSize="truncate text-heading-300"
          inline
          centerButton
          stackButtonsOnPhone={false}
          buttons={headerButtons}
          buttonSize={sizes.compact}
          testId="container-overview-title"
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

      {/* Tank cards — grouped in an elevated shadow panel */}
      <section
        className="flex flex-col gap-4 rounded-xl bg-surface-elevated-base p-6 shadow-100"
        data-testid="container-overview-tanks"
      >
        <Header title="Tanks" titleSize="text-heading-200" testId="container-overview-tanks-title" />
        <div
          className="grid grid-cols-[repeat(auto-fill,minmax(340px,1fr))] gap-4"
          data-testid="container-overview-tank-grid"
        >
          {tanks.map((tank) => (
            <TankCard
              key={tank.id}
              label={tank.label}
              cols={tank.cols}
              rows={tank.rows}
              modules={tank.modules}
              on={tank.on}
              onToggle={(on) => setPendingTank({ id: tank.id, label: tank.label, on })}
              onInfo={() => (onTankInfo ? onTankInfo(tank.id) : setStatusTankId(tank.id))}
              stats={tank.stats}
              onClick={onSelectTank ? () => onSelectTank(tank.id) : undefined}
              onModuleAction={
                onModuleAction ? (moduleIndex, action) => onModuleAction(tank.id, moduleIndex, action) : undefined
              }
            />
          ))}
        </div>
      </section>

      {/* Fan cards — grouped in an elevated shadow panel, 5×2 grid that flexes */}
      <section
        className="flex flex-col gap-4 rounded-xl bg-surface-elevated-base p-6 shadow-100"
        data-testid="container-overview-fans"
      >
        <Header title="Fans" titleSize="text-heading-200" testId="container-overview-fans-title" />
        <div
          className="grid grid-cols-2 gap-3 tablet:grid-cols-3 laptop:grid-cols-5 phone:grid-cols-1"
          data-testid="container-overview-fan-grid"
        >
          {fans.map((fan) => (
            <FanCard
              key={fan.id}
              label={fan.label}
              speedPercent={fan.speedPercent}
              speedLabel={fan.speedLabel}
              on={fan.on}
              onToggle={(on) => onToggleFan(fan.id, on)}
              onInfo={() => (onFanInfo ? onFanInfo(fan.id) : setStatusFanId(fan.id))}
            />
          ))}
        </div>
      </section>

      {controls && onToggleControl && onResetAlarm && onMuteAlarm ? (
        <ContainerControls
          controls={controls}
          onToggle={onToggleControl}
          alarm={{
            label: "Alarm",
            onReset: onResetAlarm,
            onMute: onMuteAlarm,
          }}
        />
      ) : null}

      {/* Performance — identical wiring to the rack and building detail views */}
      <section className="flex flex-col gap-4" data-testid="container-overview-performance">
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
              <span>Container</span>
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

      <TankPowerConfirmationDialog
        open={pendingTank !== null}
        label={pendingTank?.label ?? ""}
        turningOn={pendingTank?.on ?? false}
        onCancel={() => setPendingTank(null)}
        onConfirm={confirmTankToggle}
      />

      <ContainerStatusModal
        fan={statusFan}
        tank={statusTank}
        onClose={() => {
          setStatusFanId(null);
          setStatusTankId(null);
        }}
      />
    </div>
  );
};

export default ContainerOverview;
