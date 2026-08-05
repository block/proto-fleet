/**
 * The shared, backend-agnostic single-miner view. It renders purely from a
 * `SingleMinerSnapshot` — it knows nothing about where the data came from.
 *
 * Strategy 1 (fleet-native) and Strategy 3 (adapter) both feed this exact
 * component; that's the point — the only thing that varies between them is the
 * data plumbing, which the DataPathRibbon makes visible.
 */
import { ReactNode, useState } from "react";

import type {
  AsicHealth,
  DataPathStep,
  MinerControlAction,
  MinerStatus,
  SingleMinerActions,
  SingleMinerSnapshot,
} from "./types";

const STATUS_STYLES: Record<MinerStatus, { label: string; className: string }> = {
  mining: { label: "Mining", className: "bg-intent-success-10 text-text-success" },
  paused: { label: "Paused", className: "bg-intent-warning-10 text-text-warning" },
  offline: { label: "Offline", className: "bg-surface-5 text-text-primary-50" },
  error: { label: "Error", className: "bg-intent-critical-10 text-text-critical" },
};

const ASIC_HEALTH_STYLES: Record<AsicHealth, string> = {
  ok: "bg-intent-success-fill/80",
  warn: "bg-intent-warning-fill/80",
  error: "bg-intent-critical-fill/80",
  off: "bg-surface-5",
};

function formatNumber(value: number | null, digits = 1): string {
  if (value === null || Number.isNaN(value)) return "—";
  return value.toLocaleString(undefined, { maximumFractionDigits: digits });
}

function DataPathRibbon({ steps, source }: { steps: DataPathStep[]; source: string }) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border-5 bg-surface-5 p-3">
      <div className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Data path — {source}</div>
      <div className="flex flex-wrap items-center gap-2">
        {steps.map((step, i) => (
          <div key={`${step.label}-${i}`} className="flex items-center gap-2">
            <div className="rounded-md bg-surface-elevated-base px-2.5 py-1.5">
              <div className="text-200 text-text-primary">{step.label}</div>
              {step.detail ? <div className="text-heading-100 text-text-primary-50">{step.detail}</div> : null}
            </div>
            {i < steps.length - 1 ? <span className="text-text-primary-30">→</span> : null}
          </div>
        ))}
      </div>
    </div>
  );
}

function KpiTile({ label, value, unit }: { label: string; value: number | null; unit: string }) {
  return (
    <div className="flex flex-col gap-1 rounded-lg border border-border-5 bg-surface-elevated-base p-4">
      <div className="text-heading-100 tracking-wide text-text-primary-50 uppercase">{label}</div>
      <div className="flex items-baseline gap-1">
        <span className="text-heading-300 text-text-primary">{formatNumber(value)}</span>
        <span className="text-200 text-text-primary-50">{unit}</span>
      </div>
    </div>
  );
}

function IdentityRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex justify-between gap-4 py-1">
      <span className="text-200 text-text-primary-50">{label}</span>
      <span className="text-200 text-text-primary">{value}</span>
    </div>
  );
}

function AsicGrid({ snapshot }: { snapshot: SingleMinerSnapshot }) {
  if (snapshot.hashboards.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border-5 p-6 text-center text-200 text-text-primary-50">
        No hashboard / ASIC data available from this backend.
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      {snapshot.hashboards.map((hb) => (
        <div key={hb.serialNumber} className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <span className="text-200 text-text-primary">
              Hashboard {hb.index} <span className="text-text-primary-50">· {hb.serialNumber}</span>
            </span>
            <span className="text-heading-100 text-text-primary-50">
              {formatNumber(hb.hashrateThs)} TH/s · {formatNumber(hb.tempC, 0)} °C
            </span>
          </div>
          <div className="flex flex-wrap gap-1">
            {hb.asics.map((asic) => (
              <div
                key={asic.index}
                title={`ASIC ${asic.index} · ${formatNumber(asic.tempC, 0)} °C · ${formatNumber(asic.hashrateThs)} TH/s`}
                className={`h-5 w-5 rounded-sm ${ASIC_HEALTH_STYLES[asic.health]}`}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

const CONTROL_LABELS: Record<MinerControlAction, string> = {
  reboot: "Reboot",
  pause: "Pause mining",
  resume: "Resume mining",
};

function ControlBar({ snapshot, actions }: { snapshot: SingleMinerSnapshot; actions: SingleMinerActions }) {
  const [pending, setPending] = useState<MinerControlAction | null>(null);
  if (!actions.onControl) return null;

  const primary: MinerControlAction = snapshot.status === "mining" ? "pause" : "resume";
  const buttons: MinerControlAction[] = [primary, "reboot"];

  const run = async (action: MinerControlAction) => {
    setPending(action);
    try {
      await actions.onControl?.(action);
    } finally {
      setPending(null);
    }
  };

  return (
    <div className="flex gap-2">
      {buttons.map((action) => (
        <button
          key={action}
          type="button"
          disabled={pending !== null}
          onClick={() => run(action)}
          className="rounded-md border border-border-5 bg-surface-elevated-base px-3 py-1.5 text-200 text-text-primary hover:bg-surface-5 disabled:opacity-50"
        >
          {pending === action ? "…" : CONTROL_LABELS[action]}
        </button>
      ))}
    </div>
  );
}

export interface SingleMinerViewProps {
  snapshot: SingleMinerSnapshot;
  actions?: SingleMinerActions;
}

export function SingleMinerView({ snapshot, actions = {} }: SingleMinerViewProps) {
  const status = STATUS_STYLES[snapshot.status];
  return (
    <div className="flex flex-col gap-4">
      <DataPathRibbon steps={snapshot.dataPath} source={snapshot.source} />

      {/* Identity header */}
      <div className="flex flex-wrap items-start justify-between gap-4 rounded-lg border border-border-5 bg-surface-elevated-base p-4">
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-3">
            <span className="text-heading-200 text-text-primary">{snapshot.identity.name}</span>
            <span className={`rounded-full px-2.5 py-0.5 text-heading-100 ${status.className}`}>{status.label}</span>
          </div>
          <div className="min-w-[16rem]">
            <IdentityRow label="Model" value={snapshot.identity.model} />
            <IdentityRow label="Firmware" value={snapshot.identity.firmware} />
            <IdentityRow label="MDK" value={snapshot.identity.mdkVersion} />
            <IdentityRow label="MAC" value={snapshot.identity.macAddress} />
            <IdentityRow label="Serial" value={snapshot.identity.serialNumber} />
            {snapshot.identity.ipAddress ? <IdentityRow label="IP" value={snapshot.identity.ipAddress} /> : null}
          </div>
        </div>
        <ControlBar snapshot={snapshot} actions={actions} />
      </div>

      {/* KPI tiles */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="Hashrate" value={snapshot.kpis.hashrateThs} unit="TH/s" />
        <KpiTile label="Temperature" value={snapshot.kpis.tempC} unit="°C" />
        <KpiTile label="Power" value={snapshot.kpis.powerW} unit="W" />
      </div>

      {/* ASIC mini-grid — the stressor */}
      <div className="flex flex-col gap-3 rounded-lg border border-border-5 bg-surface-elevated-base p-4">
        <span className="text-heading-100 tracking-wide text-text-primary-50 uppercase">Hashboards / ASICs</span>
        <AsicGrid snapshot={snapshot} />
      </div>

      {snapshot.updatedAt ? (
        <div className="text-heading-100 text-text-primary-30">
          Updated {new Date(snapshot.updatedAt).toLocaleTimeString()}
        </div>
      ) : null}
    </div>
  );
}
