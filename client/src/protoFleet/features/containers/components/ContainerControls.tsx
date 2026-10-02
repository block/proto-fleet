import Button, { sizes, variants } from "@/shared/components/Button";
import Switch from "@/shared/components/Switch";

export interface ContainerToggleControl {
  id: string;
  label: string;
  on: boolean | undefined;
  /** Prominent operational readout, for example `75°F` or `60% speed`. */
  metric?: string;
}

export interface ContainerAlarmControl {
  label: string;
  onReset?: () => void;
  onMute?: () => void;
}

export interface ContainerControlsProps {
  controls: ContainerToggleControl[];
  alarm?: ContainerAlarmControl;
  onToggle: (id: string, on: boolean) => void;
}

interface ControlCardProps {
  control: ContainerToggleControl;
  onToggle: (on: boolean) => void;
}

const ControlCard = ({ control, onToggle }: ControlCardProps) => {
  return (
    <div
      className="flex min-h-24 min-w-0 items-center gap-5 rounded-2xl bg-surface-5 px-5 py-4"
      data-testid={`container-control-${control.id}`}
    >
      <div className="min-w-0 flex-1">
        <div className="text-emphasis-300 text-text-primary">{control.label}</div>
        {control.metric ? <div className="text-300 text-text-primary-70">{control.metric}</div> : null}
      </div>
      <Switch
        ariaLabel={`${control.label} power`}
        checked={control.on ?? false}
        disabled={control.on === undefined}
        setChecked={(next) => onToggle(typeof next === "function" ? next(control.on ?? false) : next)}
      />
    </div>
  );
};

const AlarmCard = ({ label, onReset, onMute }: ContainerAlarmControl) => (
  <div
    className="flex min-h-24 min-w-0 items-center gap-5 rounded-2xl bg-surface-5 px-5 py-4"
    data-testid="container-alarm"
  >
    <div className="min-w-0 flex-1">
      <div className="text-emphasis-300 text-text-primary">{label}</div>
    </div>
    <div className="flex shrink-0 gap-2">
      {onReset ? <Button text="Reset" variant={variants.secondary} size={sizes.compact} onClick={onReset} /> : null}
      {onMute ? <Button text="Mute" variant={variants.secondary} size={sizes.compact} onClick={onMute} /> : null}
    </div>
  </div>
);

/**
 * Container-only auxiliary equipment controls. The parent owns every state and
 * command callback; this composition has no device detection or store access.
 */
const ContainerControls = ({ controls, alarm, onToggle }: ContainerControlsProps) => (
  <section
    aria-label="Controls"
    className="flex flex-col gap-4 rounded-xl bg-surface-elevated-base p-6 shadow-100"
    data-testid="container-controls"
  >
    <h2 className="text-heading-200 text-text-primary">Controls</h2>
    <div className="grid grid-cols-2 gap-1 phone:grid-cols-1" data-testid="container-controls-grid">
      {controls.map((control) => (
        <ControlCard key={control.id} control={control} onToggle={(on) => onToggle(control.id, on)} />
      ))}
      {alarm ? <AlarmCard {...alarm} /> : null}
    </div>
  </section>
);

export default ContainerControls;
