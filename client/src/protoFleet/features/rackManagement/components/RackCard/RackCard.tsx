import clsx from "clsx";
import MiniRackGrid from "./MiniRackGrid";
import type { RackStatus, SlotStatus } from "./types";

interface RackCardProps {
  label: string;
  building?: string;
  cols: number;
  rows: number;
  slots: SlotStatus[];
  status: RackStatus;
  statusText: string;
  hashrate?: string;
  efficiency?: string;
  power?: string;
  temperature?: string;
  onClick?: () => void;
}

const statusDotColor: Record<RackStatus, string> = {
  healthy: "bg-intent-success-fill",
  needsAttention: "bg-intent-critical-fill",
  offline: "bg-core-accent-fill",
  sleeping: "bg-core-primary-20",
  mixed: "bg-intent-warning-fill",
  empty: "bg-transparent",
};

const RackCard = ({
  label,
  building,
  cols,
  rows,
  slots,
  status,
  statusText,
  hashrate,
  efficiency,
  power,
  temperature,
  onClick,
}: RackCardProps) => {
  const isEmpty = status === "empty";

  return (
    <div
      className={clsx("flex cursor-pointer flex-col rounded-2xl bg-surface-5 transition-opacity hover:opacity-80", {
        "cursor-default": !onClick,
      })}
      onClick={onClick}
    >
      {/* Body */}
      <div className="flex flex-1 flex-col px-5 pt-5 pb-4">
        {/* Header */}
        <div className="mb-5 flex items-center justify-between">
          <span className="text-300 text-emphasis-300">{label}</span>
          {building && <span className="text-300 text-text-primary-50">{building}</span>}
        </div>

        {/* Mini Rack Grid */}
        <div className="mb-4">
          <MiniRackGrid cols={cols} rows={rows} slots={slots} />
        </div>

        {/* Status / Assign CTA */}
        <div className="flex flex-1 items-center justify-center gap-1.5 pb-0.5">
          {isEmpty ? (
            <span className="text-300 text-text-primary-70 underline underline-offset-2">Assign miners</span>
          ) : (
            <>
              <span className={clsx("h-2 w-2 shrink-0 rounded-full", statusDotColor[status])} />
              <span className="text-300 text-text-primary-70">{statusText}</span>
            </>
          )}
        </div>
      </div>

      {/* Stats 2×2 grid */}
      <div className="grid grid-cols-2 border-t border-border-5">
        <span className="border-r border-b border-border-5 px-4 py-3.5 text-300 text-text-primary-70">
          {hashrate ?? "—"}
        </span>
        <span className="border-b border-border-5 px-4 py-3.5 text-300 text-text-primary-70">{efficiency ?? "—"}</span>
        <span className="border-r border-border-5 px-4 py-3.5 text-300 text-text-primary-70">{power ?? "—"}</span>
        <span className="px-4 py-3.5 text-300 text-text-primary-70">{temperature ?? "—"}</span>
      </div>
    </div>
  );
};

export default RackCard;
export type { RackCardProps };
