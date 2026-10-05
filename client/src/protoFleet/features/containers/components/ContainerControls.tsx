import Button, { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular/ProgressCircular";
import Switch from "@/shared/components/Switch";

export interface ContainerToggleControl {
  id: string;
  label: string;
  on: boolean | undefined;
  /** Prominent operational readout, for example `75°F` or `60% speed`. */
  metric?: string;
  stateLabel?: string;
  disabled?: boolean;
  pending?: boolean;
}

export interface ContainerAlarmControl {
  label: string;
  onReset?: () => void;
  onMute?: () => void;
}

export interface ContainerControlsProps {
  controls: ContainerToggleControl[];
  kind?: "cooling" | "auxiliary";
  alarm?: ContainerAlarmControl;
  onToggle?: (id: string, on: boolean) => void;
}

interface ControlCardProps {
  control: ContainerToggleControl;
  onToggle?: (on: boolean) => void;
}

const ControlCard = ({ control, onToggle }: ControlCardProps) => {
  return (
    <div
      className="flex min-h-24 min-w-0 items-center gap-3 rounded-xl bg-surface-overlay p-6"
      data-testid={`container-control-${control.id}`}
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-emphasis-300 text-text-primary">
          {control.label}
          {control.pending ? (
            <span role="status" aria-label={`Updating ${control.label}`} className="inline-flex shrink-0">
              <ProgressCircular size={14} indeterminate />
            </span>
          ) : null}
        </div>
        {control.stateLabel && onToggle ? (
          <div role="status" className="text-200 text-text-primary-70">
            {control.stateLabel}
          </div>
        ) : null}
        {control.metric ? <div className="text-300 text-text-primary-70">{control.metric}</div> : null}
      </div>
      {onToggle && control.on !== undefined ? (
        <Switch
          ariaLabel={`${control.label} ${control.id === "main_circuit_breaker" ? "closed" : control.id === "dry_cooler_auto" ? "enabled" : "power"}`}
          checked={control.on ?? false}
          disabled={control.disabled}
          setChecked={(next) => onToggle(typeof next === "function" ? next(control.on ?? false) : next)}
        />
      ) : (
        <span className="shrink-0 text-300 text-text-primary-70">
          {control.stateLabel ?? (control.on === undefined ? "Unknown" : control.on ? "On" : "Off")}
        </span>
      )}
    </div>
  );
};

const AlarmCard = ({ label, onReset, onMute }: ContainerAlarmControl) => (
  <div
    className="flex min-h-24 min-w-0 items-center gap-3 rounded-xl bg-surface-overlay p-6"
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
const ContainerControls = ({ controls, kind = "auxiliary", alarm, onToggle }: ContainerControlsProps) => (
  <section
    aria-label={kind === "cooling" ? "Cooling controls" : "Equipment controls"}
    data-testid={kind === "cooling" ? "container-cooling-controls" : "container-controls"}
  >
    <div
      className={
        kind === "cooling"
          ? "grid grid-cols-1 gap-1 tablet:grid-cols-2"
          : "grid grid-cols-1 gap-1 tablet:grid-cols-2 laptop:grid-cols-4"
      }
    >
      {controls.map((control) => (
        <ControlCard
          key={control.id}
          control={control}
          onToggle={onToggle && control.on !== undefined ? (on) => onToggle(control.id, on) : undefined}
        />
      ))}
      {alarm ? <AlarmCard {...alarm} /> : null}
    </div>
  </section>
);

export default ContainerControls;
