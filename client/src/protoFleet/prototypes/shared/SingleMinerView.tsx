/**
 * The shared, backend-agnostic single-miner view — the clean "hero" canvas.
 *
 * It renders purely from a `SingleMinerSnapshot`: status + three KPIs + the
 * hashboard/ASIC mini-grid + one control. It knows nothing about where the data
 * came from — the informational chrome (identity, data-path) lives in
 * <SingleMinerDetails>, surfaced through the MinerViewFrame "Details" modal so
 * it never crowds the actual view.
 *
 * Strategy 1 (fleet-native) and Strategy 3 (adapter) both feed this exact
 * component; only the data plumbing differs between them.
 */
import { useState } from "react";

import { toAsicData } from "./asicData";
import { STATUS_META } from "./status";
import type { MinerControlAction, SingleMinerActions, SingleMinerSnapshot } from "./types";
import AsicTablePreview from "@/shared/components/AsicTablePreview";
import Button, { sizes as buttonSizes, variants as buttonVariants } from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import Metric from "@/shared/components/Metric";
import StatusCircle, { variants as statusVariants } from "@/shared/components/StatusCircle";

function formatNumber(value: number | null, digits = 1): string {
  if (value === null || Number.isNaN(value)) return "—";
  return value.toLocaleString(undefined, { maximumFractionDigits: digits });
}

function kpiValue(value: number | null, unit: string) {
  return (
    <span className="flex items-baseline gap-1">
      <span>{formatNumber(value)}</span>
      <span className="text-300 text-text-primary-50">{unit}</span>
    </span>
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
    <div className="grid grid-cols-1 gap-4 phone:grid-cols-3">
      {snapshot.hashboards.map((hb) => (
        <Card
          key={hb.serialNumber}
          title={`Hashboard ${hb.index}`}
          type={cardType.default}
          bodyClassName="flex flex-col gap-2 p-3"
          headerAction={<span className="text-heading-100 text-text-primary-50">{hb.serialNumber}</span>}
        >
          <div className="text-heading-100 text-text-primary-50">
            {formatNumber(hb.hashrateThs)} TH/s · {formatNumber(hb.tempC, 0)} °C
          </div>
          <AsicTablePreview asics={toAsicData(hb.asics)} />
        </Card>
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
      {buttons.map((action, i) => (
        <Button
          key={action}
          text={CONTROL_LABELS[action]}
          onClick={() => run(action)}
          disabled={pending !== null}
          loading={pending === action}
          size={buttonSizes.compact}
          variant={i === 0 ? buttonVariants.primary : buttonVariants.secondary}
        />
      ))}
    </div>
  );
}

export interface SingleMinerViewProps {
  snapshot: SingleMinerSnapshot;
  actions?: SingleMinerActions;
}

export function SingleMinerView({ snapshot, actions = {} }: SingleMinerViewProps) {
  const status = STATUS_META[snapshot.status];
  return (
    <div className="flex flex-col gap-4">
      {/* Status header — identity/data-path live behind the details modal */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <span className="text-heading-200 text-text-primary">{snapshot.identity.name}</span>
          <span className="flex items-center text-200 text-text-primary-50">
            <StatusCircle status={status.circle} variant={statusVariants.simple} width="w-2" />
            {status.label}
          </span>
        </div>
        <ControlBar snapshot={snapshot} actions={actions} />
      </div>

      {/* KPI tiles — one row, evenly spread */}
      <Card title="Live metrics" type={cardType.default} bodyClassName="flex flex-row justify-around gap-4 p-4">
        <Metric label="Hashrate" value={kpiValue(snapshot.kpis.hashrateThs, "TH/s")} />
        <Metric label="Temperature" value={kpiValue(snapshot.kpis.tempC, "°C")} />
        <Metric label="Power" value={kpiValue(snapshot.kpis.powerW, "W")} />
      </Card>

      {/* ASIC mini-grid — the stressor; one card per hashboard, 3 to a row */}
      <AsicGrid snapshot={snapshot} />

      {snapshot.updatedAt ? (
        <div className="text-heading-100 text-text-primary-30">
          Updated {new Date(snapshot.updatedAt).toLocaleTimeString()}
        </div>
      ) : null}
    </div>
  );
}
