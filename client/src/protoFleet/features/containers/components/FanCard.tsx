import clsx from "clsx";
import { InfoInverted } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import RadialGauge from "@/shared/components/RadialGauge";
import Switch from "@/shared/components/Switch";

interface FanCardProps {
  label: string;
  /** Fan speed as a percentage of max (drives the donut fill and PWM readout), 0–100. */
  speedPercent: number | undefined;
  /** Human-readable RPM readout, e.g. "3,200". */
  speedLabel: string;
  on: boolean | undefined;
  toggleDisabled?: boolean;
  faulted?: boolean;
  onToggle?: (on: boolean) => void;
  onInfo?: () => void;
}

/**
 * Fan-donut geometry (jmarr spec): a 48×48 ring where the filled value arc
 * stands a touch proud of the low-opacity track.
 */
const GAUGE_SIZE = 48;
const GAUGE_TRACK_STROKE = 9;
const GAUGE_VALUE_STROKE = 12;

/**
 * A single fan control on the container overview: header (label + on/off
 * toggle + stroked info button) over a full-ring speed donut, with RPM and PWM
 * readouts spread across the footer. The donut follows the container spec — a
 * flat-capped ring, low-opacity orange track, orange value arc — and greys out
 * when the fan is off. Reuses the shared Switch, RadialGauge, and the stroked
 * InfoInverted icon.
 */
const FanCard = ({ label, speedPercent, speedLabel, on, toggleDisabled, onToggle, onInfo, faulted }: FanCardProps) => {
  const pwm = speedPercent === undefined ? undefined : Math.round(Math.max(0, Math.min(100, speedPercent)));

  return (
    <div className="flex min-w-0 flex-col rounded-2xl bg-surface-overlay px-5 pt-5 pb-6">
      <div className="mb-4 flex items-center justify-between gap-2">
        <span className="truncate text-300 text-emphasis-300">
          {label}
          {faulted ? <span className="block text-200 text-text-critical">Needs attention</span> : null}
        </span>
        <div className="flex shrink-0 items-center gap-3">
          {onToggle ? (
            <Switch
              ariaLabel={`${label} power`}
              checked={on ?? false}
              disabled={!onToggle || toggleDisabled || on === undefined}
              setChecked={(next) => onToggle(typeof next === "function" ? next(on ?? false) : next)}
            />
          ) : null}
          {onInfo ? (
            <button
              type="button"
              aria-label={`${label} info`}
              onClick={onInfo}
              className="rounded-full border-0 bg-core-primary-5 p-1.5 text-text-primary-70 transition-colors hover:bg-core-primary-10"
            >
              <InfoInverted width={iconSizes.small} />
            </button>
          ) : null}
        </div>
      </div>
      <div className="flex flex-1 items-center justify-center py-2">
        <RadialGauge
          value={pwm ?? 0}
          sweep={360}
          size={GAUGE_SIZE}
          strokeWidth={GAUGE_TRACK_STROKE}
          valueStrokeWidth={GAUGE_VALUE_STROKE}
          strokeLinecap="butt"
          trackOpacity={0.2}
          colorClassName={faulted ? "text-intent-critical-fill" : "text-core-accent-fill"}
          dataTestId="fan-gauge"
        />
      </div>
      <div
        className={clsx("mt-4 flex items-center gap-3 text-300 text-text-primary-70", !on && "text-text-primary-30")}
        data-testid="fan-card-stats"
      >
        <span className="flex-1 truncate text-center">{`${speedLabel} RPM`}</span>
        <span className="flex-1 truncate text-center">{pwm === undefined ? "— PWM" : `${pwm}% PWM`}</span>
      </div>
    </div>
  );
};

export default FanCard;
export type { FanCardProps };
