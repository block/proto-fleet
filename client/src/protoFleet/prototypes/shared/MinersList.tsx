/**
 * A deliberately dumbed-down "Miners tab" — a small tabular list that stands in
 * for ProtoFleet's fleet miners table. Each row (name · IP · firmware) opens
 * that miner's single-miner view. Strategy 2 lists two fake rigs (one per MDK
 * version); Strategy 3 lists the first-party rigs the fleet has discovered.
 */
import { STATUS_META } from "./status";
import type { MinerStatus } from "./types";
import { ArrowRight } from "@/shared/assets/icons";
import Card, { cardType } from "@/shared/components/Card";
import Chip from "@/shared/components/Chip";
import StatusCircle, { variants as statusVariants } from "@/shared/components/StatusCircle";

export interface MinerListItem {
  id: string;
  name: string;
  /** Badge, e.g. "MDK v1" — the axis strategy 2 makes visible. */
  mdkVersion?: string;
  ipAddress?: string;
  /** Firmware revision string, shown in its own column. */
  firmware?: string;
  status?: MinerStatus;
}

export interface MinersListProps {
  title: string;
  items: MinerListItem[];
  onSelect: (item: MinerListItem) => void;
  /** Row id currently connecting, if any. */
  busyId?: string | null;
  emptyMessage?: string;
}

const COLS = "grid grid-cols-[1.6fr_1fr_1fr_auto] items-center gap-4 px-4";

export function MinersList({ title, items, onSelect, busyId, emptyMessage }: MinersListProps) {
  return (
    <Card title={title} type={cardType.default}>
      {items.length === 0 ? (
        <div className="py-6 text-center text-200 text-text-primary-50">{emptyMessage ?? "No miners to show."}</div>
      ) : (
        <div className="flex flex-col">
          <div className={`${COLS} pt-3 pb-2 text-heading-100 tracking-wide text-text-primary-50 uppercase`}>
            <span>Name</span>
            <span>IP address</span>
            <span>Firmware</span>
            <span />
          </div>
          {items.map((m) => {
            const status = m.status ? STATUS_META[m.status] : null;
            return (
              <button
                key={m.id}
                type="button"
                onClick={() => onSelect(m)}
                disabled={busyId != null}
                className={`${COLS} border-t border-border-5 py-3 text-left hover:bg-surface-5 disabled:opacity-60`}
              >
                <span className="flex items-center gap-2">
                  {status ? (
                    <StatusCircle status={status.circle} variant={statusVariants.simple} width="w-2" removeMargin />
                  ) : null}
                  <span className="text-200 text-text-primary">{m.name}</span>
                  {m.mdkVersion ? <Chip>{m.mdkVersion}</Chip> : null}
                </span>
                <span className="text-200 text-text-primary-50">{m.ipAddress ?? "—"}</span>
                <span className="text-200 text-text-primary-50">{m.firmware ?? "—"}</span>
                <span className="flex justify-end">
                  {busyId === m.id ? (
                    <span className="text-heading-100 text-text-primary-50">Opening…</span>
                  ) : (
                    <ArrowRight className="w-4 text-text-primary-30" />
                  )}
                </span>
              </button>
            );
          })}
        </div>
      )}
    </Card>
  );
}
