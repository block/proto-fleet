import clsx from "clsx";
import MiniRackGrid from "./MiniRackGrid";
import type { SlotStatus } from "./types";

export interface StatusSegment {
  color: string;
  text: string;
}

interface RackCardProps {
  label: string;
  zone?: string;
  cols: number;
  rows: number;
  slots: SlotStatus[];
  loading?: boolean;
  statusSegments: StatusSegment[];
  hashrate?: string;
  efficiency?: string;
  power?: string;
  temperature?: string;
  onClick?: () => void;
}

const RackCard = ({
  label,
  zone,
  cols,
  rows,
  slots,
  loading,
  statusSegments,
  hashrate,
  efficiency,
  power,
  temperature,
  onClick,
}: RackCardProps) => {
  const isEmpty = !loading && (slots.length === 0 || slots.every((s) => s === "empty"));

  return (
    <div
      data-testid="rack-card"
      className={clsx(
        "flex cursor-pointer flex-col rounded-2xl bg-surface-overlay transition-opacity hover:opacity-80",
        {
          "cursor-default": !onClick,
        },
      )}
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
      onClick={onClick}
      onKeyDown={
        onClick
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onClick();
              }
            }
          : undefined
      }
    >
      {/* Body */}
      <div className="flex flex-1 flex-col px-5 pt-5 pb-4">
        {/* Header */}
        <div className="mb-5 flex items-center justify-between">
          <span data-testid="rack-card-label" className="text-300 text-emphasis-300">
            {label}
          </span>
          {zone ? (
            <span data-testid="rack-card-zone" className="text-300 text-text-primary-50">
              {zone}
            </span>
          ) : null}
        </div>

        {/* Mini Rack Grid */}
        <div className="flex flex-1 items-center justify-center">
          {loading ? (
            <div className="h-20 w-full animate-pulse rounded-lg bg-surface-10" />
          ) : (
            <MiniRackGrid cols={cols} rows={rows} slots={slots} />
          )}
        </div>

        {/* Status / Assign CTA */}
        <div className="flex items-center justify-center gap-1.5 pt-4 pb-0.5">
          {loading ? (
            <div className="h-4 w-24 animate-pulse rounded bg-surface-10" />
          ) : isEmpty ? (
            <span className="text-300 text-text-primary-70 underline underline-offset-2">Assign miners</span>
          ) : (
            <span className="flex items-center gap-1.5 text-300 text-text-primary-70">
              {statusSegments.map((seg, i) => (
                <span key={i} className="inline-flex items-center gap-1">
                  <span className={clsx("h-2 w-2 shrink-0 rounded-full", seg.color)} />
                  {seg.text}
                  {i < statusSegments.length - 1 ? "," : null}
                </span>
              ))}
            </span>
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
