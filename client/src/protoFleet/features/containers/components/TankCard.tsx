import clsx from "clsx";
import type { ModuleActionType } from "./moduleActions";
import TankModuleGrid, { type TankModuleState } from "./TankModuleGrid";
import { InfoInverted } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Switch from "@/shared/components/Switch";

interface TankCardProps {
  label: string;
  /** Module-bar grid dimensions. A tank of 16 modules renders as 8 × 2. */
  cols: number;
  rows: number;
  /** One entry per module bar (two-state: healthy vs needs-attention). */
  modules: TankModuleState[];
  on: boolean;
  onToggle: (on: boolean) => void;
  onInfo?: () => void;
  /**
   * Footer readouts spread across the row, e.g.
   * ["48/48 boards", "65.5°", "12.3 kW"].
   */
  stats: string[];
  onClick?: () => void;
  /**
   * When supplied, each module bar becomes an action-menu trigger (View +
   * Blink LEDs / Reboot / Sleep), called with the module's row-major index and
   * the chosen action.
   */
  onModuleAction?: (moduleIndex: number, action: ModuleActionType) => void;
}

/**
 * A single tank on the container overview. Its body is the distinct
 * TankModuleGrid visual (tall, spaced module bars) — the tank analogue of the
 * rack-detail RackHealthModule, deliberately NOT the dense fleet MiniRackGrid.
 * Header pairs the power toggle with a circular info button; the footer spreads
 * the readouts across the card width. When the tank is off the body dims to
 * signal the inactive state without dropping the readouts.
 */
const TankCard = ({
  label,
  cols,
  rows,
  modules,
  on,
  onToggle,
  onInfo,
  stats,
  onClick,
  onModuleAction,
}: TankCardProps) => {
  return (
    <div
      data-testid="tank-card"
      className={clsx(
        "relative flex min-w-0 flex-col rounded-2xl bg-surface-overlay transition-opacity",
        onClick ? "hover:opacity-80" : "cursor-default",
      )}
    >
      {onClick ? (
        <button
          type="button"
          aria-label={`View ${label}`}
          className="absolute inset-0 z-10 cursor-pointer rounded-2xl border-0 bg-transparent p-0 outline-none focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base"
          onClick={onClick}
        />
      ) : null}
      {/* Header: label + power toggle + info */}
      <div className="relative flex items-center justify-between gap-2 px-5 pt-5">
        <span data-testid="tank-card-label" className="truncate text-300 text-emphasis-300">
          {label}
        </span>
        <div className="relative z-20 flex shrink-0 items-center gap-3">
          <Switch
            ariaLabel={`${label} power`}
            checked={on}
            setChecked={(next) => onToggle(typeof next === "function" ? next(on) : next)}
          />
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

      {/* Module bars */}
      <div className={clsx("px-5 pt-6 pb-5", !on && "opacity-40", onModuleAction && "relative z-20")}>
        <TankModuleGrid cols={cols} rows={rows} modules={modules} label={label} onModuleAction={onModuleAction} />
      </div>

      {/* Footer readouts, spread across the row in equal-width cells */}
      <div
        className={clsx(
          "relative flex items-center gap-3 px-5 pb-5 text-300 text-text-primary-70",
          !on && "opacity-40",
        )}
        data-testid="tank-card-stats"
      >
        {stats.map((stat, i) => (
          <span key={i} className="flex-1 truncate text-center">
            {stat}
          </span>
        ))}
      </div>
    </div>
  );
};

export default TankCard;
export type { TankCardProps };
