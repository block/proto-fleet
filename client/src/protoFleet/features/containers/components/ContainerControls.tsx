import type { ComponentType } from "react";
import { Fan, Immersion, LightBulb, Notification, Thermometer } from "@/shared/assets/icons";
import type { IconProps } from "@/shared/assets/icons/types";
import Button, { sizes, variants } from "@/shared/components/Button";
import Switch from "@/shared/components/Switch";

export type ContainerControlIcon = "fan" | "pump" | "thermometer" | "light";

export interface ContainerToggleControl {
  id: string;
  label: string;
  icon: ContainerControlIcon;
  on: boolean;
  /** Prominent operational readout, for example `75°F` or `60% speed`. */
  metric?: string;
}

export interface ContainerAlarmControl {
  label: string;
  onReset: () => void;
  onMute: () => void;
}

export interface ContainerControlsProps {
  controls: ContainerToggleControl[];
  alarm: ContainerAlarmControl;
  onToggle: (id: string, on: boolean) => void;
}

const CONTROL_ICONS: Record<ContainerControlIcon, ComponentType<IconProps>> = {
  fan: Fan,
  pump: Immersion,
  thermometer: Thermometer,
  light: LightBulb,
};

interface ControlCardProps {
  control: ContainerToggleControl;
  onToggle: (on: boolean) => void;
}

const ControlCard = ({ control, onToggle }: ControlCardProps) => {
  const Icon = CONTROL_ICONS[control.icon];

  return (
    <div
      className="flex min-h-28 min-w-0 items-center gap-5 rounded-xl bg-surface-overlay px-5 py-4"
      data-testid={`container-control-${control.id}`}
    >
      <Icon className="shrink-0 text-text-primary-50" width="w-6" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-300 text-emphasis-300 text-text-primary">{control.label}</div>
        {control.metric ? <div className="truncate text-300 text-text-primary-70">{control.metric}</div> : null}
      </div>
      <Switch
        ariaLabel={`${control.label} power`}
        checked={control.on}
        setChecked={(next) => onToggle(typeof next === "function" ? next(control.on) : next)}
      />
    </div>
  );
};

const AlarmCard = ({ label, onReset, onMute }: ContainerAlarmControl) => (
  <div
    className="flex min-h-28 min-w-0 items-center gap-5 rounded-xl bg-surface-overlay px-5 py-4"
    data-testid="container-alarm"
  >
    <Notification className="shrink-0 text-text-primary-50" width="w-6" />
    <div className="min-w-0 flex-1 truncate text-300 text-emphasis-300 text-text-primary">{label}</div>
    <div className="flex shrink-0 gap-2">
      <Button text="Reset" variant={variants.secondary} size={sizes.compact} onClick={onReset} />
      <Button text="Mute" variant={variants.secondary} size={sizes.compact} onClick={onMute} />
    </div>
  </div>
);

/**
 * Container-only auxiliary equipment controls. The parent owns every state and
 * command callback; this composition has no device detection or store access.
 */
const ContainerControls = ({ controls, alarm, onToggle }: ContainerControlsProps) => (
  <section
    className="flex flex-col gap-4 rounded-xl bg-surface-elevated-base p-6 shadow-100"
    data-testid="container-controls"
  >
    <h2 className="text-heading-200 text-text-primary">Controls</h2>
    <div className="grid grid-cols-2 gap-1 phone:grid-cols-1" data-testid="container-controls-grid">
      {controls.map((control) => (
        <ControlCard key={control.id} control={control} onToggle={(on) => onToggle(control.id, on)} />
      ))}
      <AlarmCard {...alarm} />
    </div>
  </section>
);

export default ContainerControls;
