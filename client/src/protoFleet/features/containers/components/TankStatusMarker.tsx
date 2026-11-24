import clsx from "clsx";
import type { TankModuleState } from "./TankModuleGrid";

const colors: Record<TankModuleState, string> = {
  healthy: "bg-text-primary",
  attention: "bg-intent-critical-fill",
  offline: "bg-core-accent-fill",
  sleeping: "bg-text-primary-30",
  isolated: "bg-text-primary-30",
  unknown: "border border-border-30",
};

/** The same status shape and color in the tank drawing and its legend. */
export default function TankStatusMarker({ state }: { state: TankModuleState }) {
  return (
    <span
      aria-hidden="true"
      data-tank-status={state}
      className={clsx(
        "inline-block h-3.5 w-3.5 shrink-0",
        colors[state],
        state === "attention" ? "[clip-path:polygon(50%_0,100%_100%,0_100%)]" : "rounded-full",
      )}
    />
  );
}
