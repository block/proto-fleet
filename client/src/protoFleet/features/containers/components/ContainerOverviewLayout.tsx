import type { ReactNode } from "react";
import ContainerControls, { type ContainerToggleControl } from "./ContainerControls";
import FanCard from "./FanCard";
import type { ModuleActionType } from "./moduleActions";
import TankCard from "./TankCard";
import type { ModuleSlot, TankModuleState } from "./TankModuleGrid";
import Breadcrumb, { type BreadcrumbSegment } from "@/shared/components/Breadcrumb";
import Header from "@/shared/components/Header";
import type { StatProps } from "@/shared/components/Stat/types";
import Stats from "@/shared/components/Stats";
export interface ContainerTank {
  id: string;
  label: string;
  on: boolean | undefined;
  toggleDisabled?: boolean;
  cols: number;
  rows: number;
  modules: TankModuleState[];
  slots?: ModuleSlot[];
  stats: string[];
  statSubtitles?: (string | undefined)[];
  tempLabel?: string;
  powerLabel?: string;
}

export interface ContainerFan {
  id: string;
  label: string;
  on: boolean | undefined;
  toggleDisabled?: boolean;
  speedPercent: number | undefined;
  powerLabel: string;
  faulted?: boolean;
  statusLabel?: string;
  directionLabel?: string;
}

type ContainerKpi = Omit<StatProps, "size">;

export interface ContainerOverviewLayoutProps {
  breadcrumb: BreadcrumbSegment[];
  title: string;
  kpis: ContainerKpi[];
  tanks: ContainerTank[];
  fans: ContainerFan[];
  controls?: ContainerToggleControl[];
  performance?: ReactNode;
  plantControls?: ReactNode;
  notice?: ReactNode;
  fanCards?: ReactNode;
  onSelectModule?: (id: string) => void;
  onToggleTank?: (id: string, on: boolean) => void;
  onToggleFan?: (id: string, on: boolean) => void;
  onToggleControl?: (id: string, on: boolean) => void;
  onResetAlarm?: () => void;
  onMuteAlarm?: () => void;
  onTankInfo?: (id: string) => void;
  onFanInfo?: (id: string) => void;
  onSelectTank?: (id: string) => void;
  onModuleAction?: (tankId: string, moduleIndex: number, action: ModuleActionType) => void;
}

/** Display-only Fleet composition. Callers own polling, confirmations and history. */
export default function ContainerOverviewLayout(props: ContainerOverviewLayoutProps) {
  const { breadcrumb, title, kpis, tanks, fans, controls, performance } = props;
  const coolingControls = ["dry_cooler", "dry_cooler_auto"].flatMap(
    (id) => controls?.filter((control) => control.id === id) ?? [],
  );
  const auxiliaryControls = controls?.filter(
    (control) => control.id !== "dry_cooler" && control.id !== "dry_cooler_auto",
  );
  return (
    <div className="flex min-w-0 flex-col gap-8 p-4 tablet:p-8" data-testid="container-overview">
      <div className="flex flex-col gap-3">
        <div
          className="flex flex-wrap items-center justify-between gap-3"
          data-testid="container-overview-breadcrumb-row"
        >
          <Breadcrumb segments={breadcrumb} testId="container-overview-breadcrumb" />
        </div>
        <Header title={<h1>{title}</h1>} titleSize="text-heading-300" />
      </div>
      <Stats
        stats={kpis}
        size="large"
        grid="grid-cols-4 phone:grid-cols-2 phone:gap-y-6"
        gap="gap-x-10 phone:gap-6"
        padding=""
      />
      {props.notice}
      <div className="flex flex-col gap-6 rounded-xl bg-surface-elevated-base p-4 shadow-100 tablet:p-6">
        <section aria-label="Tanks">
          {tanks.length ? (
            <div
              className="grid grid-cols-1 gap-3 tablet:grid-cols-2 desktop:grid-cols-3"
              data-testid="container-overview-tank-grid"
            >
              {tanks.map((tank) => (
                <TankCard
                  key={tank.id}
                  {...tank}
                  onToggle={props.onToggleTank ? (on) => props.onToggleTank?.(tank.id, on) : undefined}
                  onInfo={props.onTankInfo ? () => props.onTankInfo?.(tank.id) : undefined}
                  onClick={props.onSelectTank ? () => props.onSelectTank?.(tank.id) : undefined}
                  onModuleAction={
                    props.onModuleAction ? (index, action) => props.onModuleAction?.(tank.id, index, action) : undefined
                  }
                  onSelectModule={props.onSelectModule}
                />
              ))}
            </div>
          ) : (
            <p>No tanks reported.</p>
          )}
        </section>
        <div className="flex flex-col gap-1">
          {coolingControls.length ? (
            <ContainerControls kind="cooling" controls={coolingControls} onToggle={props.onToggleControl} />
          ) : null}
          <section aria-label="Cooling fans">
            {props.fanCards ??
              (fans.length ? (
                <div className="grid grid-cols-2 gap-1 tablet:grid-cols-3 desktop:grid-cols-5">
                  {fans.map((fan) => (
                    <FanCard
                      key={fan.id}
                      {...fan}
                      toggleDisabled={fan.toggleDisabled}
                      onToggle={props.onToggleFan ? (on) => props.onToggleFan?.(fan.id, on) : undefined}
                      onInfo={props.onFanInfo ? () => props.onFanInfo?.(fan.id) : undefined}
                    />
                  ))}
                </div>
              ) : (
                <p>No fan readings available.</p>
              ))}
          </section>
        </div>
        {auxiliaryControls && (auxiliaryControls.length || props.onResetAlarm || props.onMuteAlarm) ? (
          <ContainerControls
            controls={auxiliaryControls}
            onToggle={props.onToggleControl}
            alarm={
              props.onResetAlarm || props.onMuteAlarm
                ? { label: "Alarm", onReset: props.onResetAlarm, onMute: props.onMuteAlarm }
                : undefined
            }
          />
        ) : null}
        {props.plantControls}
      </div>
      {performance}
    </div>
  );
}
